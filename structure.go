package protokit

// structure.go resolves the neutral structure of every node — the grouping, the
// table, the column — from the sources the package doc of schema/schema.go names,
// in that order:
//
//	1. any StructureReader a generator registered  (the annotation vocabularies,
//	                                                consulted in sorted Key order)
//	2. the plugin's LayoutResolver                 (grouping only; see resolveLayout)
//	3. protokit's own defaults                     (applied by the callers in build.go)
//
// protokit reads no annotation module of its own. Every vocabulary — including
// the neutral one two plugins agree on — arrives through a StructureReader, which
// is what keeps the engine's dependency graph independent of its consumers. See
// docs/ownership.md.
//
// Reads are memoized per node. The build walks each message twice and each field
// twice by design (once to decide the table's shape, once to build its columns),
// and multiplying that by the reader count for every annotated node is the
// difference between a linear and a quadratic-feeling build on a large descriptor
// set.
//
// Where two readers speak to the same option on one node, the first in sorted Key
// order wins and the overlap is reported as a "lint" diagnostic: setting an option
// twice in two vocabularies is how a schema drifts from what it appears to say.

import (
	"fmt"
	"sort"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/the-protobuf-project/protokit/schema"
)

// --- resolution ---
//
// Each resolver merges the registered readers in sorted Key order, keeping the
// first non-empty answer for every option and recording which vocabulary supplied
// it. The owner is what lets a later reader's duplicate be reported against the
// vocabulary that actually decided the value.

// datasourceOf resolves one file's grouping: the StructureReaders, then the
// LayoutResolver, then empty (the caller applies protokit's package-path default).
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

	var ds schema.Datasource
	// Each owner names the vocabulary that decided its option, or "" while the
	// option is still unclaimed.
	var ownDatabase, ownSchema, ownURL, ownProvider string

	for _, sr := range ctx.structs {
		r := sr.reader.ReadDatasource(d)
		ctx.saw(id, sr, "datasource.database", ownDatabase, r.Database != "")
		ctx.saw(id, sr, "datasource.schema", ownSchema, r.Schema != "")
		ctx.saw(id, sr, "datasource.url", ownURL, r.URL != "")
		ctx.saw(id, sr, "datasource.provider", ownProvider, r.Provider != "")

		if ds.Database == "" && r.Database != "" {
			ds.Database, ownDatabase = r.Database, sr.key
		}
		if ds.URL == "" && r.URL != "" {
			ds.URL, ownURL = r.URL, sr.key
		}
		if ds.Provider == "" && r.Provider != "" {
			ds.Provider, ownProvider = r.Provider, sr.key
		}
		// The schema decision has an owner as soon as a reader names a schema *or*
		// has an opinion about stripping — a reader carrying a global strip_version
		// but no explicit name still decides how the derived name is treated.
		if ownSchema == "" && (r.Schema != "" || r.SchemaStrip) {
			ds.Schema, ds.SchemaStrip, ownSchema = r.Schema, r.SchemaStrip, sr.key
		}
	}

	if ctx.layout != nil {
		lDB, lSchema, strip, ok := ctx.layout.ResolveDatasource(string(d.Package()))
		if ok {
			if ds.Database == "" {
				ds.Database = lDB
			}
			if ownSchema == "" {
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

// tableOf resolves one message's table structure from the StructureReaders.
func (ctx *buildCtx) tableOf(d protoreflect.MessageDescriptor) schema.TableStructure {
	id := schema.NodeIDOfMessage(d)
	if ts, ok := ctx.tblCache[id]; ok {
		return ts
	}

	var ts schema.TableStructure
	var ownTable, ownSkip, ownID, ownTimestamps, ownIndexes string

	for _, sr := range ctx.structs {
		r := sr.reader.ReadTable(d)
		ctx.saw(id, sr, "table.table", ownTable, r.Table != "")
		ctx.saw(id, sr, "table.skip", ownSkip, r.Skip)
		ctx.saw(id, sr, "table.id", ownID, r.ID != schema.IDUnspecified)
		ctx.saw(id, sr, "table.timestamps", ownTimestamps, r.Timestamps)
		ctx.saw(id, sr, "table.indexes", ownIndexes, len(r.Indexes) > 0)

		if ts.Table == "" && r.Table != "" {
			ts.Table, ownTable = r.Table, sr.key
		}
		// skip and timestamps are one-way: a reader may turn them on, never off.
		// Neither has a "false" that means anything but "unset", so OR-ing them
		// keeps a reader from silently un-skipping a message another skipped.
		if r.Skip && !ts.Skip {
			ts.Skip, ownSkip = true, sr.key
		}
		if r.Timestamps && !ts.Timestamps {
			ts.Timestamps, ownTimestamps = true, sr.key
		}
		if ts.ID == schema.IDUnspecified && r.ID != schema.IDUnspecified {
			ts.ID, ownID = r.ID, sr.key
		}
		if len(ts.Indexes) == 0 && len(r.Indexes) > 0 {
			ts.Indexes, ownIndexes = r.Indexes, sr.key
		}
	}

	if ctx.tblCache == nil {
		ctx.tblCache = map[schema.NodeID]schema.TableStructure{}
	}
	ctx.tblCache[id] = ts
	return ts
}

// columnOf resolves one field's column structure from the StructureReaders —
// which are also the only source of the referential actions, since nothing else
// in the build can recover them (see schema.StructureReader).
func (ctx *buildCtx) columnOf(d protoreflect.FieldDescriptor) schema.ColumnStructure {
	id := schema.NodeIDOfField(d)
	if cs, ok := ctx.colCache[id]; ok {
		return cs
	}

	var cs schema.ColumnStructure
	var ownColumn, ownSkip string

	for _, sr := range ctx.structs {
		r := sr.reader.ReadColumn(d)
		ctx.saw(id, sr, "column.column", ownColumn, r.Column != "")
		ctx.saw(id, sr, "column.skip", ownSkip, r.Skip)

		if cs.Column == "" && r.Column != "" {
			cs.Column, ownColumn = r.Column, sr.key
		}
		if r.Skip && !cs.Skip {
			cs.Skip, ownSkip = true, sr.key
		}
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
//   - a *collision*, when a vocabulary that already owns the option (owner) is
//     followed by another reader setting it too. The first in sorted Key order
//     wins, so the second's value is silently doing nothing — precisely the drift
//     that makes a schema stop meaning what it appears to say. Reported per node,
//     because each one is a distinct mistake with a distinct fix.
//
//   - a *deprecated use*, when a reader marked [schema.DeprecatedStructure] supplies
//     the option at all. Aggregated rather than reported per node: a migration
//     prompt that emits a line per field is one people silence.
//
// A reader that is not marked deprecated and does not collide is silent, which is
// the steady state — supplying referential actions is not a thing to warn about.
func (ctx *buildCtx) saw(id schema.NodeID, sr keyedStructure, option, owner string, readerSet bool) {
	if !readerSet || ctx.diags == nil {
		return
	}
	if owner != "" {
		ctx.diags.warnf("lint", "%s: %s and %s both set %s; %s wins and the %s value is ignored — remove it",
			id, owner, sr.key, shortOption(option), owner, sr.key)
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
//
// protokit names the vocabulary and the option but not the replacement: it owns no
// annotation module and so knows of none. The reader supplies that half through
// [schema.DeprecatedStructure.StructureDeprecation], which is rendered after an em
// dash.
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
		msg := fmt.Sprintf("%s sets %s on %s; that structural option is deprecated", n.key, shortOption(n.option), where)
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
