package service

// conflicts.go is the build-time route analysis: an ambiguous table fails the
// build, and a shadowed route is reported.

import (
	"fmt"

	"github.com/the-protobuf-project/protokit/service/httprule"
)

// checkRouteConflicts fails the build on an ambiguous route table.
//
// This is the check that makes a route table trustworthy, and it can only
// happen here: no single method can see the whole set. grpc-gateway resolves
// overlaps by registration order, at request time, with no report either way.
func (b *builder) checkRouteConflicts(ir *IR) error {
	var routes []*httprule.Route
	for _, svc := range ir.Services {
		for _, method := range svc.Methods {
			for _, binding := range method.Bindings {
				routes = append(routes, binding.Route)
			}
		}
	}

	if conflicts := httprule.Conflicts(routes); len(conflicts) > 0 {
		return fmt.Errorf("%w", conflicts[0])
	}

	// Shadowing is legal and often intentional, but a route that can never be
	// reached looks exactly like a route that was written wrong.
	for _, shadow := range httprule.Shadowed(routes) {
		b.diags = append(b.diags, Diagnostic{
			Rule:    "route",
			Subject: shadow.Loser.Source,
			Message: fmt.Sprintf(
				"%s %q is shadowed by %s %q; %q reaches the latter",
				shadow.Loser.HTTPMethod, shadow.Loser.Template.Raw,
				shadow.Winner.HTTPMethod, shadow.Winner.Template.Raw,
				shadow.Example,
			),
		})
	}

	httprule.SortBySpecificity(routes)
	return nil
}
