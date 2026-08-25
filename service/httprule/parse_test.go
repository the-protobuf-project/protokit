package httprule

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseShapes(t *testing.T) {
	// The four shapes matchit rejects or mishandles, which is why this parser
	// exists. See the README
	cases := []struct {
		tmpl   string
		match  []Match
		verb   string
		caps   []Capture
		fields []string
	}{
		{
			tmpl: "/v1/{name=shelves/*/books/*}",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindLiteral, Literal: "shelves"},
				{Kind: KindSingle},
				{Kind: KindLiteral, Literal: "books"},
				{Kind: KindSingle},
			},
			caps:   []Capture{{Field: []string{"name"}, Start: 1, End: 5}},
			fields: []string{"name"},
		},
		{
			tmpl: "/v1/{parent=shelves/*}/books",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindLiteral, Literal: "shelves"},
				{Kind: KindSingle},
				{Kind: KindLiteral, Literal: "books"},
			},
			caps:   []Capture{{Field: []string{"parent"}, Start: 1, End: 3}},
			fields: []string{"parent"},
		},
		{
			tmpl: "/v1/{name=**}",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindMulti},
			},
			caps:   []Capture{{Field: []string{"name"}, Start: 1, End: ToEnd}},
			fields: []string{"name"},
		},
		{
			// matchit accepts this as an ordinary route and folds ":cancel"
			// into the variable. Here the verb is its own thing.
			tmpl: "/v1/{name}:cancel",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindSingle},
			},
			verb:   "cancel",
			caps:   []Capture{{Field: []string{"name"}, Start: 1, End: 2}},
			fields: []string{"name"},
		},
		{
			tmpl: "/v1/books",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindLiteral, Literal: "books"},
			},
		},
		{
			tmpl: "/v1/shelves/{shelf.id}/books/{book.id}",
			match: []Match{
				{Kind: KindLiteral, Literal: "v1"},
				{Kind: KindLiteral, Literal: "shelves"},
				{Kind: KindSingle},
				{Kind: KindLiteral, Literal: "books"},
				{Kind: KindSingle},
			},
			caps: []Capture{
				{Field: []string{"shelf", "id"}, Start: 2, End: 3},
				{Field: []string{"book", "id"}, Start: 4, End: 5},
			},
			fields: []string{"shelf.id", "book.id"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.tmpl, func(t *testing.T) {
			tmpl, err := Parse(tc.tmpl)
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tc.tmpl, err)
			}
			r, err := Compile(tmpl)
			if err != nil {
				t.Fatalf("Compile(%q) = %v", tc.tmpl, err)
			}
			if !reflect.DeepEqual(r.Segments, tc.match) {
				t.Errorf("Match =\n\t%+v\nwant\n\t%+v", r.Segments, tc.match)
			}
			if r.Verb != tc.verb {
				t.Errorf("Verb = %q, want %q", r.Verb, tc.verb)
			}
			if !reflect.DeepEqual(r.Captures, tc.caps) {
				t.Errorf("Captures =\n\t%+v\nwant\n\t%+v", r.Captures, tc.caps)
			}
			if got := tmpl.Fields(); !reflect.DeepEqual(got, tc.fields) {
				t.Errorf("Fields = %v, want %v", got, tc.fields)
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, s := range []string{
		"/v1/books",
		"/v1/{name}",
		"/v1/{name}:cancel",
		"/v1/{name=shelves/*/books/*}",
		"/v1/{parent=shelves/*}/books",
		"/v1/{name=**}",
		"/v1/shelves/{shelf.id}/books/{book.id}:archive",
		"/v1/*/books",
	} {
		tmpl, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q) = %v", s, err)
		}
		if got := tmpl.String(); got != s {
			t.Errorf("Parse(%q).String() = %q", s, got)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{"v1/books", "must begin with"},
		{"", "must begin with"},
		{"/v1/{name=**}/books", "must be the final segment"},
		{"/v1/{name=**}/{other}", "must be the final segment"},
		{"/v1/{a}/{a}", "captured twice"},
		{"/v1/{a={b}}", "may not contain another variable"},
		{"/v1/{name", "to close the variable"},
		{"/v1/{}", "expected a field name"},
		{"/v1/{1bad}", "expected a field name"},
		{"/v1//books", "expected a path component"},
		{"/v1/books:", "expected a path component"},
	}
	for _, tc := range cases {
		t.Run(tc.tmpl, func(t *testing.T) {
			_, err := Parse(tc.tmpl)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want error containing %q", tc.tmpl, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse(%q) = %v, want error containing %q", tc.tmpl, err, tc.want)
			}
		})
	}
}
