package schema

// facet.go defines the composable half of protokit's extension model.
//
// The IR itself is neutral: it carries what every generator agrees on — the
// databases, schemas, tables, columns, relations, enums, and indexes derived from
// AIP plus the neutral vocabulary. Anything one generator knows and another does not (a SQL
// column type, a Solidity access model, a query surface) is a *facet*: a value a
// generator's own reader attaches to a node, stored in a side-table keyed by the
// node's fully-qualified proto name.
//
// Side-tables rather than fields is the whole point. Adding a facet requires no
// change to Database, Table, Column, or to protokit — a generator ships a
// FacetReader, and its targets read the values back with Facet[T]. protokit never
// interprets a facet's contents, and no generator can mutate another's.
//
// Two optional interfaces (StructureReader, Enricher) exist for the narrow cases
// where a reader must influence the build itself rather than merely annotate it;
// see their doc comments for why each is unavoidable.

import (
	"slices"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// NodeID identifies one node of the proto graph by its fully-qualified name:
//
//	file:    "bookstore/v1/bookstore.proto"   (the proto import path)
//	message: "bookstore.v1.Author"
//	field:   "bookstore.v1.Author.display_name"
//
// It is always derived from the descriptor, never from schema.Table.Name or
// schema.Column.Name. Those are *outputs* — a table rename (an explicit table-name
// annotation, a de-stuttering pass, a global-namespace qualification) would
// otherwise orphan every facet attached to it. A NodeID names the proto the node
// came from, so it survives anything the build does to the IR.
//
// It is a plain string, so it serializes: unlike the protoreflect descriptors on
// Table.Source / Column.Source, a NodeID can cross a process boundary.
type NodeID string

// NodeIDOfFile returns the NodeID for a proto file — its import path. Empty when
// fd is nil.
func NodeIDOfFile(fd protoreflect.FileDescriptor) NodeID {
	if fd == nil {
		return ""
	}
	return NodeID(fd.Path())
}

// NodeIDOfMessage returns the NodeID for a message — its fully-qualified proto
// name. Empty when md is nil.
func NodeIDOfMessage(md protoreflect.MessageDescriptor) NodeID {
	if md == nil {
		return ""
	}
	return NodeID(md.FullName())
}

// NodeIDOfField returns the NodeID for a field — its fully-qualified proto name,
// which includes the parent message. Empty when fd is nil.
func NodeIDOfField(fd protoreflect.FieldDescriptor) NodeID {
	if fd == nil {
		return ""
	}
	return NodeID(fd.FullName())
}

// FacetReader is how a generator contributes its own annotation package to the
// build without protokit knowing that package exists. Each Read* method receives
// a descriptor and returns whatever value the generator wants attached to that
// node — typically a struct of its own options.
//
// Contract:
//   - Key identifies the vocabulary ("orm.v1", "web3.v1") and namespaces the
//     facet side-table. It must be stable and unique within one run.
//   - Returning (nil, nil) contributes nothing; that is the normal result for an
//     unannotated node, and it costs no map entry.
//   - An error aborts the build, wrapped with the NodeID and the facet key.
//   - Readers run in sorted Key order, so a build's facet population is
//     deterministic regardless of registration order.
//
// A reader that needs to influence the build itself — not just annotate it — may
// additionally implement StructureReader and/or Enricher.
type FacetReader interface {
	// Key namespaces this reader's facets. Example: "orm.v1".
	Key() string

	// ReadFile returns the facet for a proto file, or nil to contribute nothing.
	ReadFile(protoreflect.FileDescriptor) (any, error)

	// ReadMessage returns the facet for a message, or nil to contribute nothing.
	ReadMessage(protoreflect.MessageDescriptor) (any, error)

	// ReadField returns the facet for a field, or nil to contribute nothing.
	ReadField(protoreflect.FieldDescriptor) (any, error)
}

// StructureReader is optionally implemented by a FacetReader that must supply
// *structure* — values protokit acts on while building, not values a target reads
// afterward.
//
// It is how *all* structure reaches protokit, including the neutral naming
// vocabulary two plugins agree on: protokit imports no annotation module of its
// own, so there is no read it performs first and no vocabulary it privileges. The
// agreement comes from the plugins sharing a reader implementation, not from
// protokit owning a proto. See docs/ownership.md.
//
// Three kinds of value arrive here:
//
//  1. The neutral vocabulary itself — the names and shapes every generator must
//     derive identically from one proto. Shipped as one reader that each plugin
//     imports rather than reimplements.
//
//  2. A deprecated vocabulary. A generator whose options predate the neutral one
//     maps them onto the same neutral structs here, so existing protos keep
//     generating while their authors migrate. Mark it [DeprecatedStructure] and
//     protokit emits a "lint" diagnostic naming the vocabulary and the option.
//
//  3. Referential actions. ON DELETE / ON UPDATE are not neutral structure, but
//     protokit consumes them mid-build: the foreign-key column of an embedded
//     child relation is *synthesized* and carries no descriptor of its own, so
//     nothing can recover the action after the fact. It has to arrive from the
//     parent's field descriptor while the embed is being materialized.
//
// Readers are consulted in sorted Key order and the first non-empty value for a
// given option wins, so the outcome does not depend on registration order. Where
// two readers set the same option, protokit keeps the first and reports the
// second as a "lint" diagnostic naming both.
//
// Every method must return a usable zero value when its option is absent.
type StructureReader interface {
	ReadDatasource(protoreflect.FileDescriptor) Datasource
	ReadTable(protoreflect.MessageDescriptor) TableStructure
	ReadColumn(protoreflect.FieldDescriptor) ColumnStructure
}

// DeprecatedStructure marks a StructureReader whose structural options are
// superseded by a newer vocabulary — the compatibility path a generator ships so
// protos written against its old options keep generating while their authors
// migrate.
//
// protokit needs the marker because it cannot tell the kinds of StructureReader
// apart on its own. One supplies structure no other vocabulary expresses
// (referential actions) and is permanent; another supplies structure a newer
// vocabulary now owns and is temporary. Only the second should nag.
//
// Whenever a marked reader supplies a value, protokit records a "lint" diagnostic
// naming the vocabulary and the option it set. It does not name the replacement:
// protokit owns no annotation module and so knows of none. The reader supplies
// that half through StructureDeprecation. The diagnostics are aggregated — one per
// (vocabulary, option) pair per run, with a count and an example node — because a
// deprecation that emits a line per field is a deprecation people silence rather
// than act on.
type DeprecatedStructure interface {
	StructureReader

	// StructureDeprecation returns a short clause appended to each diagnostic,
	// naming the replacement and any deadline. It is rendered after an em dash:
	//
	//	StructureDeprecation() = "use the matching (entity.v1.*) option; " +
	//	                         "orm.v1 structural options are removed in v2"
	//	→ "orm.v1-compat sets timestamps on bookstore.v1.Book; that structural
	//	   option is deprecated — use the matching (entity.v1.*) option; orm.v1
	//	   structural options are removed in v2"
	//
	// Return "" for no clause.
	StructureDeprecation() string
}

// Enricher is optionally implemented by a FacetReader that folds its generator's
// rendering decisions into the neutral IR — constraints, defaults, and the
// per-database settings a target reads back off Database.Opts.
//
// It runs after the core build and *before* index finalization, which is load
// bearing: protokit suppresses a synthesized single-column foreign-key index when
// the column is already covered by a primary key, a unique constraint, or an
// explicit index. A generator's Enrich is what sets those flags, so running it
// after finalization would emit a duplicate index on every unique-annotated
// foreign key.
//
// Enrichers run in sorted Key order. An Enricher may write to the IR's Databases;
// it must not write to another reader's facets.
type Enricher interface {
	Enrich(ir *IR) error
}

// LayoutResolver supplies the naming policy a plugin resolves from its own
// configuration file — which proto packages land in which database and schema,
// and whether a table whose name stutters with its schema gets renamed.
//
// This is deliberately separate from FacetReader. Reading an annotation and
// reading a config file are different concerns with different lifetimes: the
// annotation travels with the proto, the config travels with the deployment. The
// same protos generated under two different layouts *should* produce different
// database and schema names; the same protos read by two different plugins should
// not. Keeping them apart is what lets golden.IRAgreement hold plugins to the
// second rule without forbidding the first.
//
// A nil LayoutResolver is valid and means "no policy" — protokit falls back to
// its package-path defaults.
type LayoutResolver interface {
	// ResolveDatasource maps a proto package to its database and schema.
	//
	// An empty database or schema means "no opinion; use protokit's default".
	// stripVersion asks protokit to flatten a trailing API version out of the
	// schema name it derives ("bookstore_v1" → "bookstore"); it applies to
	// config-derived and resource-type-derived names, never to a schema a
	// StructureReader named outright from an annotation.
	//
	// ok reports whether this resolver has an opinion at all — not whether a
	// specific rule matched. A resolver backed by a loaded config returns true
	// even when no rule matches the package, because its global settings
	// (stripVersion) still apply. A resolver with no config returns false.
	ResolveDatasource(pkg string) (database, schema string, stripVersion bool, ok bool)

	// DedupeSchemaTable reports whether protokit should rename tables whose name
	// stutters with their schema ("booking" schema + "bookings" table, which
	// tools that join the two render as "bookingBookings").
	DedupeSchemaTable() bool
}

// IR is the complete output of a build: the neutral schema tree, plus the facet
// side-tables each registered reader contributed.
//
// Facets is keyed first by FacetReader.Key, then by NodeID. Read it through
// Facet, which handles both lookups and the type assertion. It is populated
// during the build and read-only afterward — a target may read any reader's
// facets and must mutate none.
type IR struct {
	// Databases is the neutral schema tree, in deterministic build order.
	Databases []*Database

	// Facets holds per-node, generator-specific values: Facets[key][node].
	// Nil-safe to read through Facet.
	Facets map[string]map[NodeID]any
}

// Facet returns the facet of type T that reader key attached to node id.
//
// It reports false when the IR is nil, the key was never registered, the node has
// no facet under that key, or the stored value is not a T. A missing facet is an
// ordinary outcome — most nodes carry no annotation — so callers should treat
// false as "unannotated", not as an error.
//
//	opts, ok := protokit.Facet[*ormv1.ColumnFacet](ir, "orm.v1", col.Node)
func Facet[T any](ir *IR, key string, id NodeID) (T, bool) {
	var zero T
	if ir == nil || ir.Facets == nil {
		return zero, false
	}
	byNode, ok := ir.Facets[key]
	if !ok {
		return zero, false
	}
	v, ok := byNode[id]
	if !ok {
		return zero, false
	}
	t, ok := v.(T)
	if !ok {
		return zero, false
	}
	return t, true
}

// FacetKeys returns the reader keys present in ir, sorted. Useful for
// diagnostics and for any pass that must walk every facet deterministically.
func FacetKeys(ir *IR) []string {
	if ir == nil || ir.Facets == nil {
		return nil
	}
	keys := make([]string, 0, len(ir.Facets))
	for k := range ir.Facets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FacetNodes returns the nodes carrying a facet under key, sorted. Returns nil
// when the key was never registered.
func FacetNodes(ir *IR, key string) []NodeID {
	if ir == nil || ir.Facets == nil {
		return nil
	}
	byNode, ok := ir.Facets[key]
	if !ok {
		return nil
	}
	ids := make([]NodeID, 0, len(byNode))
	for id := range byNode {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
