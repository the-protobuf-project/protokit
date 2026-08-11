package schema

import "testing"

type colFacet struct{ SQLType string }

func TestFacet(t *testing.T) {
	ir := &IR{
		Facets: map[string]map[NodeID]any{
			"orm.v1": {
				"bookstore.v1.Book.title": &colFacet{SQLType: "VARCHAR(500)"},
			},
		},
	}

	got, ok := Facet[*colFacet](ir, "orm.v1", "bookstore.v1.Book.title")
	if !ok {
		t.Fatal("Facet: not found; want the stored facet")
	}
	if got.SQLType != "VARCHAR(500)" {
		t.Errorf("SQLType = %q, want %q", got.SQLType, "VARCHAR(500)")
	}
}

// Every miss is an ordinary outcome, not an error: most nodes carry no
// annotation, and a target asks about all of them.
func TestFacetMisses(t *testing.T) {
	ir := &IR{
		Facets: map[string]map[NodeID]any{
			"orm.v1": {"bookstore.v1.Book.title": &colFacet{SQLType: "TEXT"}},
		},
	}

	tests := []struct {
		name string
		key  string
		node NodeID
	}{
		{"unknown key", "web3.v1", "bookstore.v1.Book.title"},
		{"unknown node", "orm.v1", "bookstore.v1.Book.subtitle"},
		{"empty node", "orm.v1", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := Facet[*colFacet](ir, tc.key, tc.node); ok {
				t.Error("Facet: found; want a miss")
			}
		})
	}

	// A nil IR and an IR with no facets at all are both safe to read.
	if _, ok := Facet[*colFacet](nil, "orm.v1", "x"); ok {
		t.Error("Facet(nil): found; want a miss")
	}
	if _, ok := Facet[*colFacet](&IR{}, "orm.v1", "x"); ok {
		t.Error("Facet(empty): found; want a miss")
	}
}

// Asking for the wrong type is a miss, not a panic. Two plugins can register
// facets under keys they do not coordinate on, so a target must be able to ask
// without trusting what it finds.
func TestFacetWrongType(t *testing.T) {
	ir := &IR{
		Facets: map[string]map[NodeID]any{
			"orm.v1": {"bookstore.v1.Book.title": "not a facet struct"},
		},
	}
	if _, ok := Facet[*colFacet](ir, "orm.v1", "bookstore.v1.Book.title"); ok {
		t.Error("Facet: found; want a miss on a type mismatch")
	}
}

func TestFacetKeysAndNodes(t *testing.T) {
	ir := &IR{
		Facets: map[string]map[NodeID]any{
			"web3.v1": {"a.B": 1},
			"orm.v1":  {"a.C": 1, "a.B": 1, "a.A": 1},
		},
	}

	keys := FacetKeys(ir)
	if len(keys) != 2 || keys[0] != "orm.v1" || keys[1] != "web3.v1" {
		t.Errorf("FacetKeys = %v, want [orm.v1 web3.v1] (sorted)", keys)
	}

	nodes := FacetNodes(ir, "orm.v1")
	want := []NodeID{"a.A", "a.B", "a.C"}
	if len(nodes) != len(want) {
		t.Fatalf("FacetNodes = %v, want %v", nodes, want)
	}
	for i := range want {
		if nodes[i] != want[i] {
			t.Fatalf("FacetNodes = %v, want %v (sorted)", nodes, want)
		}
	}

	if got := FacetNodes(ir, "absent"); got != nil {
		t.Errorf("FacetNodes(absent) = %v, want nil", got)
	}
	if got := FacetKeys(nil); got != nil {
		t.Errorf("FacetKeys(nil) = %v, want nil", got)
	}
}

// A nil Backend adapts to a nil pair rather than a non-nil interface holding a
// nil pointer — the classic Go trap, and one protokit.Build would otherwise hit
// on every call from a generator that passes no backend.
func TestAdaptBackendNil(t *testing.T) {
	reader, layout := AdaptBackend(nil)
	if reader != nil {
		t.Errorf("reader = %v, want nil", reader)
	}
	if layout != nil {
		t.Errorf("layout = %v, want nil", layout)
	}
}
