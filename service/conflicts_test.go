package service

import (
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/service/httprule"
)

// route builds a compiled route for the conflict tests.
func route(method, template, source string) *httprule.Route {
	r := httprule.MustCompile(template)
	r.HTTPMethod = method
	r.Source = source
	return r
}

func TestAnAmbiguousTableFailsTheBuild(t *testing.T) {
	// The canonical ambiguity: a resource-name wildcard against an id-shaped
	// path. Identical compiled shapes, nothing to separate them. grpc-gateway
	// serves whichever registered first, silently.
	b := &builder{}
	ir := &IR{Services: []*Service{{
		Methods: []*Method{
			{Bindings: []*Binding{{Route: route("GET", "/v1/{name=artists/*}", "GetArtist")}}},
			{Bindings: []*Binding{{Route: route("GET", "/v1/artists/{artist_id}", "GetArtistByID")}}},
		},
	}}}

	err := b.checkRouteConflicts(ir)
	if err == nil {
		t.Fatal("checkRouteConflicts succeeded; an ambiguous table must fail the build")
	}
	// The diagnostic has to name both routes and an example, or it is not
	// actionable.
	for _, want := range []string{"GetArtist", "GetArtistByID", "/v1/artists/x"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestShadowingIsReportedWithoutFailing(t *testing.T) {
	// Legal and often intentional, but a route that can never be reached looks
	// exactly like a route that was written wrong.
	b := &builder{}
	ir := &IR{Services: []*Service{{
		Methods: []*Method{
			{Bindings: []*Binding{{Route: route("GET", "/v1/{name=**}", "GetAny")}}},
			{Bindings: []*Binding{{Route: route("GET", "/v1/artists/{id}", "GetArtist")}}},
		},
	}}}

	if err := b.checkRouteConflicts(ir); err != nil {
		t.Fatalf("shadowing must not fail the build: %v", err)
	}
	if len(b.diags) != 1 {
		t.Fatalf("diags = %d, want 1", len(b.diags))
	}
	if b.diags[0].Rule != "route" {
		t.Errorf("diag rule = %q, want %q", b.diags[0].Rule, "route")
	}
}

func TestTheExampleProtosHaveNoConflicts(t *testing.T) {
	// The fixture the example serves must itself be unambiguous, or the proof
	// of concept is proving the wrong thing.
	ir := buildMusic(t)
	for _, diag := range ir.Diags {
		t.Errorf("unexpected diagnostic: [%s] %s: %s", diag.Rule, diag.Subject, diag.Message)
	}
}
