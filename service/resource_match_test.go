package service

import (
	"strings"
	"testing"
)

// A trailing "**" may match zero segments, but an empty segment is not an id:
// joining one would bind a resource name containing "//".
func TestTrailingMultiRejectsEmptySegments(t *testing.T) {
	p, err := ParsePattern("files/{path=**}")
	if err != nil {
		t.Fatalf("ParsePattern: %v", err)
	}

	cases := []struct {
		name string
		want bool
	}{
		{"files", true},       // zero segments is legal
		{"files/a", true},     //
		{"files/a/b", true},   //
		{"files/", false},     // one empty segment
		{"files/a//b", false}, // an empty id in the middle
		{"files//a", false},   //
	}
	for _, tc := range cases {
		got, ok := p.Match(tc.name)
		if ok != tc.want {
			t.Errorf("Match(%q) = %v, want %v (bound %v)", tc.name, ok, tc.want, got)
		}
	}
	// The zero-segment case binds an empty value rather than failing.
	if got, ok := p.Match("files"); !ok || got["path"] != "" {
		t.Errorf(`Match("files") = %v, %v; want path bound to ""`, got, ok)
	}
}

// A compound segment was parsed as one variable literally named "a}~{b".
// Refusing beats a silently wrong parse.
func TestCompoundSegmentsAreRejected(t *testing.T) {
	for _, pattern := range []string{
		"projects/{project}~{database}",
		"items/{a}_{b}",
		"items/{a}-{b}",
		"items/{a}.{b}",
	} {
		_, err := ParsePattern(pattern)
		if err == nil {
			t.Errorf("ParsePattern(%q) succeeded; compound segments are not supported", pattern)
			continue
		}
		if !strings.Contains(err.Error(), "compound segment") {
			t.Errorf("ParsePattern(%q) = %v, want a compound-segment error", pattern, err)
		}
	}
}
