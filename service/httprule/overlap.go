package httprule

// overlap.go decides the two questions route analysis rests on: can two
// templates match the same request, and if so does one outrank the other.

// Overlaps reports whether some concrete request could match both routes.
//
// Two routes for different HTTP methods or different custom verbs never
// overlap, because the request carries exactly one of each.
func Overlaps(a, b *Route) bool {
	if a.HTTPMethod != b.HTTPMethod || a.Verb != b.Verb {
		return false
	}

	am, bm := a.HasMulti(), b.HasMulti()
	fa, fb := a.fixed(), b.fixed()

	// How many leading positions both routes constrain. A "**" constrains
	// nothing past its own position, so it caps the comparison there.
	var n int
	switch {
	case !am && !bm:
		if len(a.Segments) != len(b.Segments) {
			return false
		}
		n = len(a.Segments)
	case am && !bm:
		if len(b.Segments) < fa {
			return false
		}
		n = fa
	case !am && bm:
		if len(a.Segments) < fb {
			return false
		}
		n = fb
	default:
		n = min(fa, fb)
	}

	for i := 0; i < n; i++ {
		x, y := a.Segments[i], b.Segments[i]
		// Only two literals can be mutually exclusive; every other pairing has
		// at least one wildcard, which the other's value satisfies.
		if x.Kind == KindLiteral && y.Kind == KindLiteral && x.Literal != y.Literal {
			return false
		}
	}
	return true
}

// Compare orders two routes by specificity, most specific first: -1 when a is
// strictly more specific, +1 when b is, and 0 when neither dominates.
//
// A 0 between two overlapping routes is an ambiguity, because it means no rule
// decides which one a shared request belongs to.
func Compare(a, b *Route) int {
	n := min(len(a.Segments), len(b.Segments))
	for i := 0; i < n; i++ {
		ra, rb := a.Segments[i].Kind.rank(), b.Segments[i].Kind.rank()
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
	}

	// Every position they share is equally specific; fall back to the shape.
	if am, bm := a.HasMulti(), b.HasMulti(); am != bm {
		if !am {
			return -1 // a pins its length, b does not
		}
		return 1
	}
	if len(a.Segments) != len(b.Segments) {
		if len(a.Segments) > len(b.Segments) {
			return -1 // more segments pinned
		}
		return 1
	}
	if av, bv := a.Verb != "", b.Verb != ""; av != bv {
		if av {
			return -1 // a custom verb is an extra constraint
		}
		return 1
	}
	return 0
}
