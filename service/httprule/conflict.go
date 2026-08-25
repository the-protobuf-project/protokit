package httprule

// conflict.go reports what overlap analysis found: genuine ambiguities, which
// fail the build, and resolved shadowing, which is legal but worth surfacing.

import (
	"fmt"
	"sort"
	"strings"
)

// Conflict is a pair of routes that overlap without either dominating: some
// request matches both and nothing decides which one serves it.
type Conflict struct {
	A, B *Route

	// Example is a concrete path that both routes match, so the diagnostic can
	// show the problem rather than describe it.
	Example string
}

func (c Conflict) Error() string {
	return fmt.Sprintf(
		"ambiguous routes: %s %q (%s) and %s %q (%s) both match %q, and neither is more specific",
		c.A.HTTPMethod, c.A.Template.Raw, c.A.Source,
		c.B.HTTPMethod, c.B.Template.Raw, c.B.Source,
		c.Example,
	)
}

// Conflicts returns every ambiguous pair in a route set.
//
// This is the check that makes a route table trustworthy. grpc-gateway resolves
// overlapping patterns by registration order, at request time, with no report;
// with the whole set in hand at build time, an API that cannot be routed
// unambiguously fails to generate instead.
//
// Overlap alone is not a conflict. /v1/{name=books/*} and /v1/books:batchGet
// overlap, and the literal-beats-wildcard rule decides between them; that pair
// is reported by [Shadowed], not here.
func Conflicts(routes []*Route) []Conflict {
	var out []Conflict
	for i := 0; i < len(routes); i++ {
		for j := i + 1; j < len(routes); j++ {
			a, b := routes[i], routes[j]
			if Overlaps(a, b) && Compare(a, b) == 0 {
				out = append(out, Conflict{A: a, B: b, Example: example(a, b)})
			}
		}
	}
	return out
}

// Shadowing is an overlapping pair that precedence does resolve: Winner serves
// every request the two share.
type Shadowing struct {
	Winner, Loser *Route
	Example       string
}

// Shadowed returns the overlapping pairs precedence resolves. They are legal
// and often intentional — a custom verb beside a wildcard get — but a generator
// should surface them, because a route that never receives a request looks
// exactly like a route that was written wrong.
func Shadowed(routes []*Route) []Shadowing {
	var out []Shadowing
	for i := 0; i < len(routes); i++ {
		for j := i + 1; j < len(routes); j++ {
			a, b := routes[i], routes[j]
			if !Overlaps(a, b) {
				continue
			}
			switch Compare(a, b) {
			case -1:
				out = append(out, Shadowing{Winner: a, Loser: b, Example: example(a, b)})
			case 1:
				out = append(out, Shadowing{Winner: b, Loser: a, Example: example(b, a)})
			}
		}
	}
	return out
}

// SortBySpecificity orders a route table most specific first, which is the
// order a runtime scans it in. Ties break on the source name so the emitted
// table is byte-identical across runs.
func SortBySpecificity(routes []*Route) {
	sort.SliceStable(routes, func(i, j int) bool {
		if c := Compare(routes[i], routes[j]); c != 0 {
			return c < 0
		}
		return routes[i].Source < routes[j].Source
	})
}

// example builds a concrete path matching both routes, preferring each
// position's literal so the result reads like a real request.
func example(a, b *Route) string {
	n := max(len(a.Segments), len(b.Segments))
	var segs []string
	for i := 0; i < n; i++ {
		switch {
		case i < len(a.Segments) && a.Segments[i].Kind == KindLiteral:
			segs = append(segs, a.Segments[i].Literal)
		case i < len(b.Segments) && b.Segments[i].Kind == KindLiteral:
			segs = append(segs, b.Segments[i].Literal)
		case i < len(a.Segments) && a.Segments[i].Kind == KindMulti,
			i < len(b.Segments) && b.Segments[i].Kind == KindMulti:
			segs = append(segs, "x")
		default:
			segs = append(segs, "x")
		}
	}
	path := "/" + strings.Join(segs, "/")
	if a.Verb != "" {
		path += ":" + a.Verb
	}
	return path
}
