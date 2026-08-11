package protokit

// structure.go resolves the neutral structure of every node — the grouping, the
// table, the column — from the sources the package doc of schema/schema.go names,
// in that order:
//
//	1. protokit.v1.datasource / .table / .column   (this file reads them directly)
//	2. any StructureReader a generator registered  (a legacy vocabulary, and the
//	                                                referential actions protokit.v1
//	                                                deliberately does not express)
//	3. the plugin's LayoutResolver                 (grouping only; see resolveLayout)
//	4. protokit's own defaults                     (applied by the callers in build.go)
//
// Reads are memoized per node. The build walks each message twice and each field
// twice by design (once to decide the table's shape, once to build its columns),
// and multiplying that by the reader count for every annotated node is the
// difference between a linear and a quadratic-feeling build on a large descriptor
// set.
//
// Where a StructureReader and protokit.v1 both speak to the same field, protokit.v1
// wins and the overlap is reported as a "lint" diagnostic: setting an option twice
// in two vocabularies is how a schema drifts from what it appears to say.

import (
	"fmt"
	"sort"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/protobuf/protokitpbv1"
	"github.com/the-protobuf-project/protokit/schema"
)

// --- protokit.v1 option accessors ---
//
// Each returns an empty message when the descriptor is nil or unannotated, so
// every caller can read through the getters without a presence check.

func pkDatasourceOpts(d protoreflect.FileDescriptor) *protokitpbv1.DatasourceOptions {
	if d == nil || !proto.HasExtension(d.Options(), protokitpbv1.E_Datasource) {
		return &protokitpbv1.DatasourceOptions{}
	}
	return proto.GetExtension(d.Options(), protokitpbv1.E_Datasource).(*protokitpbv1.DatasourceOptions)
}

func pkTableOpts(d protoreflect.MessageDescriptor) *protokitpbv1.TableOptions {
	if d == nil || !proto.HasExtension(d.Options(), protokitpbv1.E_Table) {
		return &protokitpbv1.TableOptions{}
	}
	return proto.GetExtension(d.Options(), protokitpbv1.E_Table).(*protokitpbv1.TableOptions)
}

func pkColumnOpts(d protoreflect.FieldDescriptor) *protokitpbv1.ColumnOptions {
	if d == nil || !proto.HasExtension(d.Options(), protokitpbv1.E_Column) {
		return &protokitpbv1.ColumnOptions{}
	}
	return proto.GetExtension(d.Options(), protokitpbv1.E_Column).(*protokitpbv1.ColumnOptions)
}

// pkIDStrategy maps protokit.v1's IdStrategy onto the neutral schema.IDStrategy.
func pkIDStrategy(s protokitpbv1.IdStrategy) schema.IDStrategy {
	switch s {
	case protokitpbv1.IdStrategy_ID_STRATEGY_ULID:
		return schema.IDULID
	case protokitpbv1.IdStrategy_ID_STRATEGY_UUID:
		return schema.IDUUID
	default:
		return schema.IDUnspecified
	}
}

// pkIndexes converts protokit.v1 index declarations to the neutral IR form.
func pkIndexes(defs []*protokitpbv1.IndexDef) []*schema.Index {
	if len(defs) == 0 {
		return nil
	}
	out := make([]*schema.Index, 0, len(defs))
	for _, d := range defs {
		out = append(out, &schema.Index{
			Name:    d.GetIndex(),
			Columns: d.GetColumns(),
			Unique:  d.GetUnique(),
		})
	}
	return out
}

// --- resolution ---

// datasourceOf resolves one file's grouping: protokit.v1.datasource, then any
// StructureReader, then the LayoutResolver, then empty (the caller applies
// protokit's package-path default).
//
// The (Schema, SchemaStrip) pair moves as a unit. Whichever source decides the
// schema also decides whether a trailing API version is stripped from it, because
// the two answers are only meaningful together: an explicitly annotated schema is
// authoritative and never stripped, while a config-derived or resource-derived one
// obeys the layout's strip_version. Splitting them would let a config strip a name
// the author wrote out in full.
func (ctx *buildCtx) datasourceOf(d protoreflect.FileDescriptor) schema.Datasource {
	id := schema.NodeIDOfFile(d)
	if ds, ok := ctx.dsCache[id]; ok {
		return ds
	}

	o := pkDatasourceOpts(d)
	ds := schema.Datasource{
		Database: o.GetDatabase(),
		Schema:   o.GetSchema(),
		URL:      o.GetUrl(),
		Provider: o.GetProvider(),
		// SchemaStrip stays false: an annotated schema is authoritative.
	}
	// schemaClaimed tracks whether the schema decision has an owner yet. A source
	// claims it by naming a schema *or* by having an opinion about stripping — a
	// layout with a global strip_version but no matching rule still decides how the
	// resource-derived name is treated.
	schemaClaimed := ds.Schema != ""

	for _, sr := range ctx.structs {
		r := sr.reader.ReadDatasource(d)
		ctx.saw(id, sr, "datasource.database", ds.Database != "", r.Database != "")
		ctx.saw(id, sr, "datasource.schema", ds.Schema != "", r.Schema != "")
		ctx.saw(id, sr, "datasource.url", ds.URL != "", r.URL != "")
		ctx.saw(id, sr, "datasource.provider", ds.Provider != "", r.Provider != "")

		if ds.Database == "" {
			ds.Database = r.Database
		}
		if ds.URL == "" {
			ds.URL = r.URL
		}
		if ds.Provider == "" {
			ds.Provider = r.Provider
		}
		if !schemaClaimed && (r.Schema != "" || r.SchemaStrip) {
			ds.Schema, ds.SchemaStrip = r.Schema, r.SchemaStrip
			schemaClaimed = true
		}
	}

	if ctx.layout != nil {
		lDB, lSchema, strip, ok := ctx.layout.ResolveDatasource(string(d.Package()))
		if ok {
			if ds.Database == "" {
				ds.Database = lDB
			}
			if !schemaClaimed {
				ds.Schema, ds.SchemaStrip = lSchema, strip
			}
		}
	}

	if ctx.dsCache == nil {
		ctx.dsCache = map[schema.NodeID]schema.Datasource{}
	}
	ctx.dsCache[id] = ds
	return ds
}

// tableOf resolves one message's table structure: protokit.v1.table, then any
// StructureReader for the fields it leaves unset.
func (ctx *buildCtx) tableOf(d protoreflect.MessageDescriptor) schema.TableStructure {
	id := schema.NodeIDOfMessage(d)
	if ts, ok := ctx.tblCache[id]; ok {
		return ts
	}

	o := pkTableOpts(d)
	ts := schema.TableStructure{
		Table:      o.GetTable(),
		Skip:       o.GetSkip(),
		ID:         pkIDStrategy(o.GetId()),
		Timestamps: o.GetTimestamps(),
		Indexes:    pkIndexes(o.GetIndexes()),
	}

	for _, sr := range ctx.structs {
		r := sr.reader.ReadTable(d)
		ctx.saw(id, sr, "table.table", ts.Table != "", r.Table != "")
		ctx.saw(id, sr, "table.skip", ts.Skip, r.Skip)
		ctx.saw(id, sr, "table.id", ts.ID != schema.IDUnspecified, r.ID != schema.IDUnspecified)
		ctx.saw(id, sr, "table.timestamps", ts.Timestamps, r.Timestamps)
		ctx.saw(id, sr, "table.indexes", len(ts.Indexes) > 0, len(r.Indexes) > 0)

		if ts.Table == "" {
			ts.Table = r.Table
		}
		// skip and timestamps are one-way: a reader may turn them on, never off.
		// Neither has a "false" that means anything but "unset", so OR-ing them
		// keeps a reader from silently un-skipping a message another skipped.
		ts.Skip = ts.Skip || r.Skip
		ts.Timestamps = ts.Timestamps || r.Timestamps
		if ts.ID == schema.IDUnspecified {
			ts.ID = r.ID
		}
		if len(ts.Indexes) == 0 {
			ts.Indexes = r.Indexes
		}
	}

	if ctx.tblCache == nil {
		ctx.tblCache = map[schema.NodeID]schema.TableStructure{}
	}
	ctx.tblCache[id] = ts
	return ts
}

// columnOf resolves one field's column structure: protokit.v1.column for the name
// and skip, then any StructureReader — which is also the only source of the
// referential actions, since protokit.v1 does not express them.
func (ctx *buildCtx) columnOf(d protoreflect.FieldDescriptor) schema.ColumnStructure {
	id := schema.NodeIDOfField(d)
	if cs, ok := ctx.colCache[id]; ok {
		return cs
	}

	o := pkColumnOpts(d)
	cs := schema.ColumnStructure{
		Column: o.GetColumn(),
		Skip:   o.GetSkip(),
	}

	for _, sr := range ctx.structs {
		r := sr.reader.ReadColumn(d)
		ctx.saw(id, sr, "column.column", cs.Column != "", r.Column != "")
		ctx.saw(id, sr, "column.skip", cs.Skip, r.Skip)

		if cs.Column == "" {
			cs.Column = r.Column
		}
		cs.Skip = cs.Skip || r.Skip
		if cs.OnDelete == "" {
			cs.OnDelete = r.OnDelete
		}
		if cs.OnUpdate == "" {
			cs.OnUpdate = r.OnUpdate
		}
	}

	if ctx.colCache == nil {
		ctx.colCache = map[schema.NodeID]schema.ColumnStructure{}
	}
	ctx.colCache[id] = cs
	return cs
}

// saw records what one reader had to say about one option on one node, raising
// the two diagnostics that matter:
//
//   - a *collision*, when protokit.v1 and a reader both set the option. protokit.v1
//     wins, so the reader's value is silently doing nothing — precisely the drift
//     that makes a schema stop meaning what it appears to say. Reported per node,
//     because each one is a distinct mistake with a distinct fix.
//
//   - a *deprecated use*, when a reader marked [schema.DeprecatedStructure] supplies
//     the option at all. Aggregated rather than reported per node: a migration
//     prompt that emits a line per field is one people silence.
//
// A reader that is not marked deprecated and does not collide is silent, which is
// the steady state — supplying referential actions is not a thing to warn about.
func (ctx *buildCtx) saw(id schema.NodeID, sr keyedStructure, option string, neutralSet, readerSet bool) {
	if !readerSet || ctx.diags == nil {
		return
	}
	if neutralSet {
		ctx.diags.warnf("lint", "%s: both protokit.v1.%s and %s set %s; protokit.v1 wins and the %s value is ignored — remove it",
			id, option, sr.key, shortOption(option), sr.key)
		return
	}
	if sr.deprecated {
		ctx.noteDeprecated(id, sr, option)
	}
}

// noteDeprecated folds one deprecated use into the run's aggregate for that
// (vocabulary, option) pair.
func (ctx *buildCtx) noteDeprecated(id schema.NodeID, sr keyedStructure, option string) {
	k := sr.key + "/" + option
	n, ok := ctx.deprecations[k]
	if !ok {
		n = &deprecationNote{key: sr.key, option: option, note: sr.note, first: id}
		ctx.deprecations[k] = n
	}
	n.count++
}

// flushDeprecations emits one diagnostic per deprecated (vocabulary, option) pair,
// in sorted key order so the output is stable across runs.
func (ctx *buildCtx) flushDeprecations() {
	if ctx.diags == nil || len(ctx.deprecations) == 0 {
		return
	}
	keys := make([]string, 0, len(ctx.deprecations))
	for k := range ctx.deprecations {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		n := ctx.deprecations[k]
		where := string(n.first)
		if n.count > 1 {
			where = fmt.Sprintf("%s and %d other node(s)", n.first, n.count-1)
		}
		msg := fmt.Sprintf("%s sets %s on %s; use protokit.v1.%s instead", n.key, shortOption(n.option), where, n.option)
		if n.note != "" {
			msg += " — " + n.note
		}
		ctx.diags.warnf("lint", "%s", msg)
	}
}

// shortOption is the bare option name for a "datasource.database"-style path,
// used in the diagnostic's trailing clause.
func shortOption(option string) string {
	for i := len(option) - 1; i >= 0; i-- {
		if option[i] == '.' {
			return option[i+1:]
		}
	}
	return option
}
