package protokit

// embed.go makes the proto→IR mapping lossless for nested data messages.
//
// The base build pass (build.go) turns only google.api.resource messages into
// tables and renders any message-typed field as a JSONB/Json blob. That drops
// the structure of embedded messages (a calendar Event's Location, Schedule,
// Recurrence, …) and produces almost no relations. Here we instead:
//
//  1. record every normalizable message-typed field as an "embed request"
//     during the build (no JSONB column is emitted for it), then
//  2. walk those requests, materializing each referenced message as its own
//     table (reachable-from-resource ⇒ a model) and synthesizing the scalar FK
//     column + foreign key that connects parent and child.
//
// Singular field → belongs-to (FK on the parent). Repeated field → has-many
// (FK on the child pointing back at the parent). Cycles terminate because a
// message is materialized at most once per database.
//
// A materialized child lands in the parent's schema by default, but the
// generator's layout config (read via the Backend for the child message's own
// file) can route it to a fixed schema instead (see materialize /
// valueObjectSchema): this is how a shared value object such as google.type.Money
// is collected into one "common" schema deterministically rather than wherever it
// happens to be seen first.

import (
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"

	"github.com/the-protobuf-project/protokit/naming"
	"github.com/the-protobuf-project/protokit/schema"
	"github.com/the-protobuf-project/protokit/types"
)

// buildCtx carries the cross-file state the lossless build needs: every proto
// message by full name (so a child defined in another file can be materialized),
// the queue of embed requests collected while building tables, and the structure
// sources (see structure.go) every pass resolves names through.
type buildCtx struct {
	msgIndex map[string]*protogen.Message
	embeds   []*embedReq
	m2m      []*m2mReq // repeated resource_reference fields awaiting join-table synthesis

	// structs are the registered StructureReaders in sorted Key order, each paired
	// with its key for diagnostics. Consulted in sorted Key order; first answer wins.
	structs []keyedStructure
	// layout is the plugin's naming policy; nil means "no policy, use defaults".
	layout schema.LayoutResolver
	// diags collects the deprecated-twin lint raised during resolution.
	diags *diagnostics

	// Structure reads are memoized: the build visits each message and each field
	// more than once by design, and re-reading options per visit per reader adds up.
	dsCache  map[schema.NodeID]schema.Datasource
	tblCache map[schema.NodeID]schema.TableStructure
	colCache map[schema.NodeID]schema.ColumnStructure

	// deprecations aggregates the deprecated-vocabulary uses seen during the
	// build, keyed "<reader key>/<option>", and flushed as one diagnostic per
	// entry. See noteDeprecated.
	deprecations map[string]*deprecationNote
}

// deprecationNote counts uses of one deprecated option and remembers the first
// node that used it, so the flushed diagnostic can be specific without being one
// line per field.
type deprecationNote struct {
	key    string // the reader's facet key ("orm.v1-compat")
	option string // the option path ("table.timestamps")
	note   string // the reader's deprecation clause
	first  schema.NodeID
	count  int
}

// keyedStructure pairs a StructureReader with the facet key it was registered
// under, so a diagnostic can name the vocabulary an option came from, plus the
// deprecation note when the reader is a compatibility shim.
type keyedStructure struct {
	key        string
	reader     schema.StructureReader
	deprecated bool
	note       string // from schema.DeprecatedStructure.StructureDeprecation
}

// embedReq is one message-typed field that must become a relation.
type embedReq struct {
	db         *schema.Database
	schemaName string          // schema the parent lives in; the child joins it
	parent     *schema.Table   // table carrying the field
	field      *protogen.Field // the message-typed field
	targetMsg  string          // referenced message full name
	repeated   bool
	optional   bool
	onDelete   string
	onUpdate   string
}

// newBuildCtx indexes every message (including nested) across all files in the
// request — both the generate-flagged files and their imported dependencies — so
// an embedded child can be materialized regardless of which file defines it.
// Indexing imports is what lets an imported value type (google.type.Money, a
// vendored common.proto) be relationalized into a table; protoc ships the full
// transitive descriptor set in the request, so no source or network fetch is
// needed. Unreferenced imported messages cost only a map entry: a message is
// only materialized when a field actually relationalizes to it.
//
// readers arrive already sorted by Key; the StructureReaders among them are
// extracted once here so every structure resolution walks a small slice instead
// of type-asserting the full reader set per node.
func newBuildCtx(p *protogen.Plugin, diags *diagnostics, readers []schema.FacetReader, layout schema.LayoutResolver) *buildCtx {
	idx := map[string]*protogen.Message{}
	var walk func(msgs []*protogen.Message)
	walk = func(msgs []*protogen.Message) {
		for _, m := range msgs {
			idx[string(m.Desc.FullName())] = m
			walk(m.Messages)
		}
	}
	for _, f := range p.Files {
		walk(f.Messages)
	}

	var structs []keyedStructure
	for _, r := range readers {
		sr, ok := r.(schema.StructureReader)
		if !ok {
			continue
		}
		ks := keyedStructure{key: r.Key(), reader: sr}
		if d, ok := r.(schema.DeprecatedStructure); ok {
			ks.deprecated, ks.note = true, d.StructureDeprecation()
		}
		structs = append(structs, ks)
	}

	return &buildCtx{
		msgIndex:     idx,
		structs:      structs,
		layout:       layout,
		diags:        diags,
		dsCache:      map[schema.NodeID]schema.Datasource{},
		tblCache:     map[schema.NodeID]schema.TableStructure{},
		colCache:     map[schema.NodeID]schema.ColumnStructure{},
		deprecations: map[string]*deprecationNote{},
	}
}

// normalizableMessage returns the referenced message's full name when field f is
// a message-typed field that should be normalized into a related table, or ""
// otherwise. Maps and non-relationalizable messages — well-known types with a
// native scalar mapping (Timestamp, Duration, the wrappers, …) and the freeform
// google.protobuf wrappers (Struct, Any, …) — are not normalized; they keep
// their scalar / JSONB mapping. Every other message is, including imported value
// types such as google.type.Money (see types.Relationalizable).
func normalizableMessage(f *protogen.Field) string {
	md := f.Desc.Message()
	if md == nil || f.Desc.IsMap() {
		return ""
	}
	full := string(md.FullName())
	if !types.Relationalizable(full) {
		return ""
	}
	return full
}

// isRequiredField reports whether f carries REQUIRED or IDENTIFIER field_behavior.
func isRequiredField(f *protogen.Field) bool {
	for _, b := range fieldBehaviors(f) {
		if b == annotations.FieldBehavior_REQUIRED || b == annotations.FieldBehavior_IDENTIFIER {
			return true
		}
	}
	return false
}

// normalizeEmbeds drains the embed queue, materializing child tables and wiring
// FK columns. Processing a child may enqueue further embeds (its own message
// fields), so it loops until the queue is empty; each message is materialized
// once per database, which also breaks reference cycles.
func (ctx *buildCtx) normalizeEmbeds(diags *diagnostics) {
	for i := 0; i < len(ctx.embeds); i++ {
		req := ctx.embeds[i]
		msg := ctx.msgIndex[req.targetMsg]
		if msg == nil {
			// The descriptor set lacks the referenced message (no source for it was
			// supplied to protoc). Keep the data rather than drop the field: fall
			// back to a JSONB column, and surface the gap instead of staying silent.
			diags.warnf("ref", "table %q field %q references message %q which is not in the "+
				"descriptor set; kept as a JSONB column", req.parent.Name, req.field.Desc.Name(), req.targetMsg)
			req.parent.Columns = append(req.parent.Columns, &schema.Column{
				Name:     string(req.field.Desc.Name()),
				Comment:  cleanComment(req.field.Comments.Leading),
				Type:     schema.TypeJSON,
				Optional: true,
			})
			continue
		}
		child := ctx.materialize(req.db, req.schemaName, msg)

		if req.repeated {
			// has-many: the child carries a FK back to the parent.
			fkCol := naming.SnakeCase(req.parent.ModelName) + "_id"
			addFKColumn(child, fkCol, req.parent.ModelName, req.parent.ProtoMessage, true, embedAction(req.onDelete, true), req.onUpdate)
		} else {
			// belongs-to: the parent carries the FK to the child.
			fkCol := string(req.field.Desc.Name()) + "_id"
			addFKColumn(req.parent, fkCol, child.ModelName, child.ProtoMessage, !req.optional, embedAction(req.onDelete, !req.optional), req.onUpdate)
		}
	}
}

// materialize returns the table for msg in db, building it on first sight. The
// build records any further embeds the child introduces.
//
// The child's schema defaults to schemaName (the parent's schema) but is
// overridden by the generator's layout config for the child message's own file: a
// value object shared across services (google.type.Money, a vendored common
// type) can thus be routed to a single, deterministic schema (e.g. "common")
// regardless of which parent materializes it first. The child always stays in
// the parent's database (db) — an FK target can't cross databases — so a rule's
// database is irrelevant here; only its schema applies.
func (ctx *buildCtx) materialize(db *schema.Database, schemaName string, msg *protogen.Message) *schema.Table {
	full := string(msg.Desc.FullName())
	for _, s := range db.Schemas {
		for _, t := range s.Tables {
			if t.ProtoMessage == full {
				return t
			}
		}
	}

	schemaName = ctx.valueObjectSchema(schemaName, msg)
	s := schemaByName(db, schemaName)
	srcPath := msg.Desc.ParentFile().Path()
	ts := ctx.tableOf(msg.Desc)
	name := ts.Table
	if name == "" {
		name = naming.SnakePlural(string(msg.Desc.Name()))
	}
	t := &schema.Table{
		Name:         name,
		Comment:      cleanComment(msg.Comments.Leading),
		ModelName:    string(msg.Desc.Name()),
		LocalName:    string(msg.Desc.Name()),
		ProtoMessage: full,
		SourceFile:   sourceFileBase(srcPath),
		SourceProto:  srcPath,
		SourceDir:    protoDirNoVersion(srcPath),
		PgSchema:     schemaName,
		ValueObject:  true, // materialized from an embedded message, not a resource
		Source:       msg.Desc,
		Node:         schema.NodeIDOfMessage(msg.Desc),
	}
	// Append before populating columns so a self/cyclic reference finds the table.
	s.Tables = append(s.Tables, t)
	ctx.populateColumns(db, s, t, msg)
	applyAIPSystemFields(t)
	applyIDStrategy(t, ts.ID)
	applyTimestamps(t, ts.Timestamps)
	appendDeclaredIndexes(t, ts.Indexes)
	ensurePK(t)
	return t
}

// valueObjectSchema returns the schema a materialized value object should live
// in: the schema the backend assigns to the message's own file (via the
// generator's layout config), or fallback (the parent's schema) when the backend
// assigns none. A config-derived schema obeys the same version-stripping
// treatment as resource-derived schemas in mergeFile.
func (ctx *buildCtx) valueObjectSchema(fallback string, msg *protogen.Message) string {
	ds := ctx.datasourceOf(msg.Desc.ParentFile())
	s := ds.Schema
	if s == "" {
		return fallback
	}
	if ds.SchemaStrip {
		s = naming.StripPackageVersion(s)
	}
	return s
}
