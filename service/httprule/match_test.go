package httprule

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// split mirrors README §1.2 step 2: split the raw path on "/", dropping
// only the empty piece the leading slash produces.
func split(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		tmpl string
		path string
		verb string
		want map[string]string
		ok   bool
	}{
		{
			tmpl: "/v1/{name=shelves/*/books/*}",
			path: "/v1/shelves/s1/books/b1",
			want: map[string]string{"name": "shelves/s1/books/b1"},
			ok:   true,
		},
		{tmpl: "/v1/{name=shelves/*/books/*}", path: "/v1/shelves/s1/books", ok: false},
		{tmpl: "/v1/{name=shelves/*/books/*}", path: "/v1/shelves/s1/books/b1/pages/p1", ok: false},
		{
			tmpl: "/v1/{parent=shelves/*}/books",
			path: "/v1/shelves/s1/books",
			want: map[string]string{"parent": "shelves/s1"},
			ok:   true,
		},
		{
			// "**" matches zero segments.
			tmpl: "/v1/{name=**}", path: "/v1",
			want: map[string]string{"name": ""}, ok: true,
		},
		{
			tmpl: "/v1/{name=**}", path: "/v1/a/b/c",
			want: map[string]string{"name": "a/b/c"}, ok: true,
		},
		{
			tmpl: "/v1/{name}:cancel", path: "/v1/op1", verb: "cancel",
			want: map[string]string{"name": "op1"}, ok: true,
		},
		{tmpl: "/v1/{name}:cancel", path: "/v1/op1", verb: "", ok: false},
		{tmpl: "/v1/{name}", path: "/v1/op1", verb: "cancel", ok: false},
		{
			// A percent-escaped slash stays inside its segment: the value is
			// returned still encoded, and decoding happens after the match.
			tmpl: "/v1/{name=shelves/*}", path: "/v1/shelves/a%2Fb",
			want: map[string]string{"name": "shelves/a%2Fb"}, ok: true,
		},
		{tmpl: "/v1/{name}", path: "/v1/", ok: false}, // "*" needs a non-empty component
		{tmpl: "/v1/books", path: "/v1/books", want: map[string]string{}, ok: true},
		{tmpl: "/v1/books", path: "/v2/books", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.tmpl+" "+tc.path+":"+tc.verb, func(t *testing.T) {
			r := MustCompile(tc.tmpl)
			got, ok := r.MatchPath(split(tc.path), tc.verb)
			if ok != tc.ok {
				t.Fatalf("MatchPath(%q) ok = %v, want %v", tc.path, ok, tc.ok)
			}
			if ok && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MatchPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestDecodeSegment(t *testing.T) {
	cases := []struct {
		in, want, wantErr string
	}{
		{in: "plain", want: "plain"},
		{in: "a%20b", want: "a b"},
		{in: "a%3Ab", want: "a:b"},
		// Multi-byte UTF-8 is percent-encoded one byte at a time.
		{in: "caf%C3%A9", want: "café"},
		// %2F is left as written, in either case. README §1.2
		{in: "a%2Fb", want: "a%2Fb"},
		{in: "a%2fb", want: "a%2fb"},
		{in: "a%2", wantErr: "truncated percent-escape"},
		{in: "a%", wantErr: "truncated percent-escape"},
		{in: "a%zz", wantErr: "not two hex digits"},
		{in: "a%FF", wantErr: "invalid UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := DecodeSegment(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("DecodeSegment(%q) = %q, want error containing %q", tc.in, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("DecodeSegment(%q) = %v, want error containing %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeSegment(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("DecodeSegment(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestMatchDecodes covers the reference path: match, then decode. These cases
// are mirrored verbatim in http-rs/grpc-http/src/route/tests.rs, which is the
// only thing keeping the two implementations from drifting.
func TestMatchDecodes(t *testing.T) {
	cases := []struct {
		tmpl, path, verb string
		want             map[string]string
		wantErr          string
	}{
		{
			tmpl: "/v1/{name=shelves/*/books/*}", path: "/v1/shelves/s1/books/b1",
			want: map[string]string{"name": "shelves/s1/books/b1"},
		},
		{
			// A %2F stays encoded, so this stays a two-segment name and cannot
			// be confused with the three-segment "shelves/a/b".
			tmpl: "/v1/{name=shelves/*}", path: "/v1/shelves/a%2Fb",
			want: map[string]string{"name": "shelves/a%2Fb"},
		},
		{
			tmpl: "/v1/{name=shelves/*}", path: "/v1/shelves/caf%C3%A9",
			want: map[string]string{"name": "shelves/café"},
		},
		{
			tmpl: "/v1/{name=**}", path: "/v1/a%20b/c",
			want: map[string]string{"name": "a b/c"},
		},
		{
			tmpl: "/v1/{name=shelves/*}", path: "/v1/racks/s1",
			wantErr: ErrNoMatch.Error(),
		},
		{
			// Matching succeeds and decoding fails: a 400, not a 404.
			tmpl: "/v1/{name=shelves/*}", path: "/v1/shelves/b%2",
			wantErr: "truncated percent-escape",
		},
	}

	for _, tc := range cases {
		t.Run(tc.tmpl+" "+tc.path, func(t *testing.T) {
			r := MustCompile(tc.tmpl)
			got, err := r.Match(split(tc.path), tc.verb)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Match(%q) = %v, want error containing %q", tc.path, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Match(%q) = %v, want error containing %q", tc.path, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Match(%q) = %v", tc.path, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Match(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// A decode failure must name the field, so the 400 can carry a FieldViolation
// pointing at what the client actually sent.
func TestMatchDecodeErrorNamesField(t *testing.T) {
	r := MustCompile("/v1/{book.name=shelves/*}")
	_, err := r.Match(split("/v1/shelves/b%2"), "")
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("Match() = %v, want a *DecodeError", err)
	}
	if de.Field != "book.name" {
		t.Errorf("Field = %q, want %q", de.Field, "book.name")
	}
}
