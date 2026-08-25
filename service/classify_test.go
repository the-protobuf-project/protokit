package service

import "testing"

// A method named GetBook with a POST additional_binding is not a Get. Trusting
// the first binding alone marked it non-mutating, which would exempt it from
// any policy written against a mutating selector.
func TestEveryBindingMustAgreeWithThePattern(t *testing.T) {
	binding := func(method string) *Binding { return &Binding{HTTPMethod: method} }

	cases := []struct {
		name     string
		pattern  MethodPattern
		bindings []*Binding
		want     bool
	}{
		{"read pattern, read binding", PatternGet, []*Binding{binding("GET")}, false},
		// The classifier turns this into Custom, but mutating must be safe even
		// if a caller hands it the optimistic pattern directly.
		{"read pattern, write binding", PatternGet, []*Binding{binding("POST")}, true},
		{"read pattern, mixed bindings", PatternGet,
			[]*Binding{binding("GET"), binding("DELETE")}, true},
		{"write pattern", PatternCreate, []*Binding{binding("POST")}, true},
		// A custom method bound only to GET is the one case where the author
		// has said unambiguously that it does not mutate.
		{"custom, GET only", PatternCustom, []*Binding{binding("GET")}, false},
		{"custom, POST", PatternCustom, []*Binding{binding("POST")}, true},
		// Nothing to inspect: assume the worst for a custom method, and leave a
		// standard read a read.
		{"custom, no bindings", PatternCustom, nil, true},
		{"read pattern, no bindings", PatternGet, nil, false},
	}
	for _, tc := range cases {
		if got := mutating(tc.pattern, tc.bindings); got != tc.want {
			t.Errorf("%s: mutating(%v) = %v, want %v", tc.name, tc.pattern, got, tc.want)
		}
	}
}

// AIP-231 through AIP-235 define the batch methods with custom verbs, so a verb
// must not disqualify them the way it disqualifies a named standard method.
func TestBatchPatternsSurviveTheirCustomVerbs(t *testing.T) {
	for _, p := range []MethodPattern{
		PatternBatchGet, PatternBatchCreate, PatternBatchUpdate, PatternBatchDelete,
	} {
		if p == PatternCustom {
			t.Errorf("%v collapsed into Custom", p)
		}
	}
	// BatchGet reads; the rest write.
	if PatternBatchGet.mutating() {
		t.Error("BatchGet should not be mutating")
	}
	for _, p := range []MethodPattern{PatternBatchCreate, PatternBatchUpdate, PatternBatchDelete} {
		if !p.mutating() {
			t.Errorf("%v should be mutating", p)
		}
	}
}
