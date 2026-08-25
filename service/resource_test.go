package service

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePattern(t *testing.T) {
	cases := []struct {
		pat  string
		vars []string
	}{
		{"shelves/{shelf}", []string{"shelf"}},
		{"shelves/{shelf}/books/{book}", []string{"shelf", "book"}},
		{"projects/{project}/locations/{location}/settings", []string{"project", "location"}},
		{"files/{path=**}", []string{"path"}},
		{"settings", nil},
	}
	for _, tc := range cases {
		t.Run(tc.pat, func(t *testing.T) {
			p, err := ParsePattern(tc.pat)
			if err != nil {
				t.Fatalf("ParsePattern(%q) = %v", tc.pat, err)
			}
			if got := p.Vars(); !reflect.DeepEqual(got, tc.vars) {
				t.Errorf("Vars() = %v, want %v", got, tc.vars)
			}
			if got := p.String(); got != tc.pat {
				t.Errorf("String() = %q, want %q", got, tc.pat)
			}
		})
	}
}

func TestParsePatternErrors(t *testing.T) {
	cases := []struct{ pat, want string }{
		{"", "empty resource pattern"},
		{"/shelves/{shelf}", "must not begin with"},
		{"shelves//{shelf}", "empty segment"},
		{"shelves/{}", "unnamed variable"},
		{"shelves/{a=*}", `only "**" is allowed`},
		{"files/{path=**}/more", "must be the final segment"},
		{"shelves/{shelf", "malformed variable"},
	}
	for _, tc := range cases {
		t.Run(tc.pat, func(t *testing.T) {
			_, err := ParsePattern(tc.pat)
			if err == nil {
				t.Fatalf("ParsePattern(%q) succeeded, want error containing %q", tc.pat, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParsePattern(%q) = %v, want error containing %q", tc.pat, err, tc.want)
			}
		})
	}
}

func TestPatternMatch(t *testing.T) {
	cases := []struct {
		pat  string
		name string
		want map[string]string
		ok   bool
	}{
		{"shelves/{shelf}/books/{book}", "shelves/s1/books/b1",
			map[string]string{"shelf": "s1", "book": "b1"}, true},
		// AIP-122: a resource name is relative. A leading slash is a different
		// string, and accepting it would give one resource two names.
		{"shelves/{shelf}/books/{book}", "/shelves/s1/books/b1", nil, false},
		{"shelves/{shelf}/books/{book}", "shelves/s1/books", nil, false},
		{"shelves/{shelf}/books/{book}", "shelves/s1/books/b1/pages/p1", nil, false},
		{"shelves/{shelf}/books/{book}", "shelves//books/b1", nil, false},
		{"shelves/{shelf}/books/{book}", "racks/s1/books/b1", nil, false},
		{"shelves/{shelf}", "shelves/s1", map[string]string{"shelf": "s1"}, true},
		{"files/{path=**}", "files/a/b/c", map[string]string{"path": "a/b/c"}, true},
		{"files/{path=**}", "files", map[string]string{"path": ""}, true},
		{"settings", "settings", map[string]string{}, true},
		{"shelves/{shelf}", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.pat+" vs "+tc.name, func(t *testing.T) {
			p, err := ParsePattern(tc.pat)
			if err != nil {
				t.Fatalf("ParsePattern(%q) = %v", tc.pat, err)
			}
			got, ok := p.Match(tc.name)
			if ok != tc.ok {
				t.Fatalf("Match(%q) ok = %v, want %v", tc.name, ok, tc.ok)
			}
			if ok && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Match(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestResourceDepthOrdersParentsFirst(t *testing.T) {
	shelf := &Resource{Type: "library.example.com/Shelf"}
	book := &Resource{Type: "library.example.com/Book"}
	for _, s := range []string{"shelves/{shelf}"} {
		p, _ := ParsePattern(s)
		shelf.Patterns = append(shelf.Patterns, p)
	}
	for _, s := range []string{"shelves/{shelf}/books/{book}"} {
		p, _ := ParsePattern(s)
		book.Patterns = append(book.Patterns, p)
	}
	if shelf.Depth() >= book.Depth() {
		t.Errorf("depth: shelf %d, book %d — a parent must sort before its child",
			shelf.Depth(), book.Depth())
	}
}
