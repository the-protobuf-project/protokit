package httprule

import (
	"reflect"
	"testing"
)

func route(method, tmpl, source string) *Route {
	r := MustCompile(tmpl)
	r.HTTPMethod = method
	r.Source = source
	return r
}

func TestConflicts(t *testing.T) {
	cases := []struct {
		name    string
		routes  []*Route
		wantN   int
		example string
	}{
		{
			// The canonical ambiguity: a resource-name wildcard against an
			// id-shaped path. Identical compiled shapes, nothing to separate
			// them. grpc-gateway serves whichever registered first.
			name: "wildcard against id",
			routes: []*Route{
				route("GET", "/v1/{name=books/*}", "GetBook"),
				route("GET", "/v1/books/{book_id}", "GetBookByID"),
			},
			wantN:   1,
			example: "/v1/books/x",
		},
		{
			name: "different http methods never conflict",
			routes: []*Route{
				route("GET", "/v1/{name=books/*}", "GetBook"),
				route("DELETE", "/v1/books/{book_id}", "DeleteBook"),
			},
			wantN: 0,
		},
		{
			name: "different verbs never conflict",
			routes: []*Route{
				route("POST", "/v1/{name=books/*}:archive", "ArchiveBook"),
				route("POST", "/v1/{name=books/*}:publish", "PublishBook"),
			},
			wantN: 0,
		},
		{
			name: "literal beats wildcard, so no conflict",
			routes: []*Route{
				route("GET", "/v1/{name=**}", "GetAny"),
				route("GET", "/v1/books/{book_id}", "GetBook"),
			},
			wantN: 0,
		},
		{
			name: "verb-bearing route does not conflict with its verbless twin",
			routes: []*Route{
				route("POST", "/v1/{name=books/*}:archive", "ArchiveBook"),
				route("POST", "/v1/{name=books/*}", "UpdateBook"),
			},
			wantN: 0,
		},
		{
			name: "two multis at the same depth",
			routes: []*Route{
				route("GET", "/v1/{name=**}", "GetAny"),
				route("GET", "/v1/{path=**}", "GetPath"),
			},
			wantN:   1,
			example: "/v1/x",
		},
		{
			name: "disjoint literals",
			routes: []*Route{
				route("GET", "/v1/books/{id}", "GetBook"),
				route("GET", "/v1/shelves/{id}", "GetShelf"),
			},
			wantN: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Conflicts(tc.routes)
			if len(got) != tc.wantN {
				for _, c := range got {
					t.Logf("conflict: %v", c)
				}
				t.Fatalf("Conflicts() found %d, want %d", len(got), tc.wantN)
			}
			if tc.wantN > 0 && got[0].Example != tc.example {
				t.Errorf("Example = %q, want %q", got[0].Example, tc.example)
			}
		})
	}
}

func TestShadowed(t *testing.T) {
	routes := []*Route{
		route("GET", "/v1/{name=**}", "GetAny"),
		route("GET", "/v1/books/{book_id}", "GetBook"),
	}
	got := Shadowed(routes)
	if len(got) != 1 {
		t.Fatalf("Shadowed() found %d, want 1", len(got))
	}
	if got[0].Winner.Source != "GetBook" || got[0].Loser.Source != "GetAny" {
		t.Errorf("winner/loser = %s/%s, want GetBook/GetAny",
			got[0].Winner.Source, got[0].Loser.Source)
	}
}

func TestSortBySpecificity(t *testing.T) {
	routes := []*Route{
		route("GET", "/v1/{name=**}", "d"),
		route("GET", "/v1/books/{id}", "b"),
		route("GET", "/v1/books/featured", "a"),
		route("GET", "/v1/{parent=shelves/*}/books", "c"),
	}
	SortBySpecificity(routes)

	// "a" is all literals; "d" ends in "**" and is least specific. Between
	// them, "c" precedes "b" because every position the two share is equally
	// specific and "c" pins a fourth segment. That pair cannot overlap — they
	// differ at index 1, "shelves" against "books" — so their relative order is
	// a determinism concern rather than a routing one.
	want := []string{"a", "c", "b", "d"}
	var got []string
	for _, r := range routes {
		got = append(got, r.Source)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// A route table's emitted order must not depend on the order bindings happened
// to be registered in, or generated output would differ run to run.
func TestSortIsDeterministic(t *testing.T) {
	build := func() []*Route {
		return []*Route{
			route("GET", "/v1/{name=**}", "d"),
			route("GET", "/v1/{other=**}", "e"),
			route("GET", "/v1/books/{id}", "b"),
			route("GET", "/v1/books/featured", "a"),
		}
	}
	first := build()
	SortBySpecificity(first)

	shuffled := build()
	shuffled[0], shuffled[3] = shuffled[3], shuffled[0]
	SortBySpecificity(shuffled)

	for i := range first {
		if first[i].Source != shuffled[i].Source {
			t.Fatalf("position %d: %s vs %s", i, first[i].Source, shuffled[i].Source)
		}
	}
}
