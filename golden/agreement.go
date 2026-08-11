package golden

// agreement.go holds IRAgreement, the test for protokit's central claim: that two
// different plugins reading the same protos derive the same *neutral* names.
//
// This is the property the facet model exists to protect. Before it, a generator
// resolved the database, schema, table, and column names from its own annotation
// package and its own config — so pointing a second generator at one set of protos
// produced two artifacts that disagreed about what things were called, and nothing
// caught it. Now protokit reads protokit.v1 itself and each generator contributes
// only facets, which by construction cannot move a name.
//
// "By construction" is a claim about code, and claims about code rot. IRAgreement
// is how it stays true.

import (
	"fmt"
	"sort"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"
)

// maxReportedDivergences caps the failure report. A structural divergence tends to
// cascade — one renamed table moves every foreign key pointing at it — and the
// first handful localize the cause as well as a hundred would.
const maxReportedDivergences = 20

// IRAgreement builds dir's case under two plugins and asserts they derive the same
// neutral schema: database, schema, table, and column names, primary keys, and
// foreign-key resolution. Facets are not compared — differing there is the entire
// point of having two plugins.
//
// Divergences are reported by NodeID (the fully-qualified proto name), never by
// generated name, so the report points at the annotation responsible rather than
// at the symptom.
//
// Both plugins must be given the same LayoutResolver. Layout is deployment policy
// — which packages land in which database — and two different layouts *should*
// produce different names; only annotation reading is under test here. A mismatch
// is called out in the failure message, since it is the likeliest cause of a
// confusing failure.
func IRAgreement(t *testing.T, dir string, a, b protokit.Plugin) {
	t.Helper()
	req := BuildRequest(t, dir)

	irA := buildIR(t, req, a, "A")
	irB := buildIR(t, req, b, "B")

	divergences := diffNeutral(irA, irB)
	if len(divergences) == 0 {
		return
	}

	msg := fmt.Sprintf("the two plugins derive %d different neutral name(s) from the same protos "+
		"— a facet reader must not change what anything is called:", len(divergences))
	if a.Layout != b.Layout {
		msg += "\n  note: the plugins were given different LayoutResolvers; layout decides naming " +
			"policy, so pass the same one to both unless that difference is what you are testing."
	}
	for i, d := range divergences {
		if i == maxReportedDivergences {
			msg += fmt.Sprintf("\n  … and %d more", len(divergences)-i)
			break
		}
		msg += "\n  " + d
	}
	t.Error(msg)
}

// buildIR runs one plugin's build against the shared request. label names the
// plugin in a build failure, which is otherwise hard to attribute.
func buildIR(t *testing.T, req *pluginpb.CodeGeneratorRequest, pl protokit.Plugin, label string) *protokit.IR {
	t.Helper()
	p, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	ir, err := protokit.Build(p, protokit.Options{}, pl.Readers, pl.Layout)
	if err != nil {
		t.Fatalf("plugin %s: build IR: %v", label, err)
	}
	return ir
}

// diffNeutral compares the two IRs' neutral projections and returns one
// human-readable line per divergence, sorted for a stable report.
func diffNeutral(a, b *protokit.IR) []string {
	fa, fb := neutralFacts(a), neutralFacts(b)

	keys := map[string]bool{}
	for k := range fa {
		keys[k] = true
	}
	for k := range fb {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var out []string
	for _, k := range sorted {
		va, okA := fa[k]
		vb, okB := fb[k]
		switch {
		case !okA:
			out = append(out, fmt.Sprintf("%s: absent in plugin A, %q in plugin B", k, vb))
		case !okB:
			out = append(out, fmt.Sprintf("%s: %q in plugin A, absent in plugin B", k, va))
		case va != vb:
			out = append(out, fmt.Sprintf("%s: %q in plugin A, %q in plugin B", k, va, vb))
		}
	}
	return out
}

// neutralFacts flattens an IR into the comparable projection: every neutral name
// the build derived, keyed by a coordinate that is stable across the very
// divergence being tested.
//
// That stability is why the keys are built from NodeID rather than from generated
// names. Keying a table's facts by its Name would mean a divergent name changes the
// key too, and the two IRs would report "a table that only exists in A" plus "a
// table that only exists in B" instead of "this table is named differently" — the
// same information, minus the part that matters.
//
// Tables protokit synthesizes (many-to-many join tables) map to no message and have
// no NodeID, so they fall back to a positional coordinate; a divergence in one of
// those does surface as an add/remove pair, which is the honest reading, since a
// synthesized table has no identity apart from where it sits.
func neutralFacts(ir *protokit.IR) map[string]string {
	facts := map[string]string{}
	if ir == nil {
		return facts
	}
	for _, db := range ir.Databases {
		for _, s := range db.Schemas {
			for _, t := range s.Tables {
				key := tableKey(db.Name, s.Name, t)
				facts[key+"/database"] = db.Name
				facts[key+"/schema"] = s.Name
				facts[key+"/table"] = t.Name
				facts[key+"/primary_key"] = t.PKColumn

				for _, c := range t.Columns {
					facts[columnKey(key, c)+"/column"] = c.Name
				}
				for _, fk := range t.ForeignKeys {
					fkKey := fmt.Sprintf("%s/fk[%s]", key, fk.Column)
					facts[fkKey+"/references"] = fmt.Sprintf("%s.%s(%s)",
						fk.ReferencedSchema, fk.ReferencedTable, fk.ReferencedColumn)
				}
			}
		}
	}
	return facts
}

// tableKey is a table's stable coordinate: its proto message name where it has
// one, else its position in the schema tree.
func tableKey(dbName, schemaName string, t *schema.Table) string {
	if t.Node != "" {
		return string(t.Node)
	}
	return fmt.Sprintf("<synthesized %s/%s/%s>", dbName, schemaName, t.Name)
}

// columnKey is a column's stable coordinate: its proto field name where it has
// one, else its name qualified by the owning table's key. Synthesized columns (the
// surrogate id, audit timestamps, embed foreign keys) have no field to name them,
// and their names are protokit's own — so a divergence there would be a bug in the
// engine rather than in a plugin.
func columnKey(tableKey string, c *schema.Column) string {
	if c.Node != "" {
		return string(c.Node)
	}
	return fmt.Sprintf("%s/<synthesized %s>", tableKey, c.Name)
}
