package httprule

import (
	"strings"
	"testing"
)

// The diagnostic's example path must actually be matched by both routes it
// names, or it demonstrates nothing. Taking the longer route's length
// unconditionally produced a path the shorter, fixed-length route rejects.
func TestExamplePathIsMatchedByBothRoutes(t *testing.T) {
	cases := [][2]string{
		{"/v1/{name=**}", "/v1"},
		{"/v1/{name=**}", "/v1/artists/{id}"},
		{"/v1/{name=artists/*}", "/v1/artists/{id}"},
		{"/v1/{name=**}", "/v1/{other=**}"},
		{"/v1/{p=artists/*}/tracks", "/v1/artists/{a}/tracks"},
	}
	for _, tc := range cases {
		a, b := MustCompile(tc[0]), MustCompile(tc[1])
		a.HTTPMethod, b.HTTPMethod = "GET", "GET"

		if !Overlaps(a, b) {
			continue // nothing to demonstrate
		}
		path := example(a, b)
		segs := strings.Split(strings.TrimPrefix(path, "/"), "/")

		if !a.matches(segs, a.Verb) {
			t.Errorf("example(%q, %q) = %q, which %q does not match", tc[0], tc[1], path, tc[0])
		}
		if !b.matches(segs, b.Verb) {
			t.Errorf("example(%q, %q) = %q, which %q does not match", tc[0], tc[1], path, tc[1])
		}
	}
}
