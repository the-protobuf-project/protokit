package spec

import (
	"os"
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/graphql/dialect"
	"github.com/the-protobuf-project/protokit/graphql/introspect"
)

// TestSmoke parses GRAPHQL_SMOKE (comma-separated SDL paths) and reports stats.
func TestSmoke(t *testing.T) {
	env := os.Getenv("GRAPHQL_SMOKE")
	if env == "" {
		t.Skip("GRAPHQL_SMOKE not set")
	}
	for _, p := range strings.Split(env, ",") {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		in, err := introspect.ParseSDL(p, string(src))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, ft := range in.Types {
			names = append(names, ft.Name)
		}
		s := Build(in, dialect.Detect(names))
		gen, paged := 0, 0
		for _, ty := range s.Types {
			if ty.Generated {
				gen++
			}
		}
		for _, o := range s.Operations {
			if o.Pagination != nil {
				paged++
			}
		}
		for _, ty := range s.Types {
			if ty.Generated {
				t.Logf("  generated: %s (%s)", ty.Name, ty.Kind)
			}
		}
		t.Logf("%s: types=%d generated=%d ops=%d paged=%d groups=%d", p, len(s.Types), gen, len(s.Operations), paged, len(s.Groups))
	}
}
