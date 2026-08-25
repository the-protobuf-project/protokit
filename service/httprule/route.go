package httprule

import "strings"

// Match is one element of a compiled template: a flat, positional matcher with
// no nesting left in it. Kind is never KindVariable — [Compile] expands
// variables into their sub-segments and records the spans separately.
type Match struct {
	Kind    Kind
	Literal string // set when Kind is KindLiteral
}

// Capture records where one variable's value lives in a matched path, as a
// half-open span of segment indices.
//
// Indices count from the start of the path, which is well defined precisely
// because "**" may only appear last: every span except one ending in "**" has a
// fixed position regardless of how long the request path turns out to be.
type Capture struct {
	// Field is the dotted request-message field path this span binds.
	Field []string

	// Start is the first segment index of the span.
	Start int

	// End is one past the last segment index, or [ToEnd] when the span ends in
	// a "**" and so extends to whatever the path's final segment is.
	End int
}

// ToEnd marks a capture span that runs to the end of the path.
const ToEnd = -1

// Name returns the capture's field path joined with ".".
func (c Capture) Name() string { return strings.Join(c.Field, ".") }

// Route is a template compiled for matching.
//
// HTTPMethod and Source are not derived from the template; a caller sets them
// before conflict analysis so diagnostics can name the binding a route came
// from, and so routes for different HTTP methods are not compared.
type Route struct {
	Template *Template

	// Segments is the flattened match sequence. No element is KindVariable:
	// Compile expands variables into their sub-segments and records the spans
	// in Captures.
	Segments []Match

	Verb     string
	Captures []Capture

	HTTPMethod string // "GET", "POST", …
	Source     string // e.g. "library.v1.LibraryService.GetBook binding 0"
}

// Compile flattens a parsed template into a Route.
func Compile(t *Template) (*Route, error) {
	r := &Route{Template: t, Verb: t.Verb}

	for _, s := range t.Segments {
		if s.Kind != KindVariable {
			r.Segments = append(r.Segments, Match{Kind: s.Kind, Literal: s.Literal})
			continue
		}

		start := len(r.Segments)
		multi := false
		for _, sub := range s.Sub {
			if sub.Kind == KindMulti {
				multi = true
			}
			r.Segments = append(r.Segments, Match{Kind: sub.Kind, Literal: sub.Literal})
		}

		end := len(r.Segments)
		if multi {
			end = ToEnd
		}
		r.Captures = append(r.Captures, Capture{Field: s.Field, Start: start, End: end})
	}
	return r, nil
}

// MustCompile is Compile over Parse, panicking on failure. It is for tests and
// for templates that are literals in this repository's own source.
func MustCompile(s string) *Route {
	t, err := Parse(s)
	if err != nil {
		panic(err)
	}
	r, err := Compile(t)
	if err != nil {
		panic(err)
	}
	return r
}

// HasMulti reports whether the route ends in a "**".
func (r *Route) HasMulti() bool {
	return len(r.Segments) > 0 && r.Segments[len(r.Segments)-1].Kind == KindMulti
}

// fixed returns the number of leading segments that must match one-to-one. It
// is the full length, less the trailing "**" when there is one.
func (r *Route) fixed() int {
	if r.HasMulti() {
		return len(r.Segments) - 1
	}
	return len(r.Segments)
}

// MatchPath matches a request's path segments against the route and returns the
// captured spans keyed by dotted field path.
//
// segs are the raw, still-percent-encoded segments, split on "/" — the encoding
// is preserved because decoding before matching would let a "%2F" invent a
// segment boundary. Callers decode each returned value afterwards, which is
// what README §1.2 requires.
//
// The returned values are still encoded. [Route.Match] is the reference
// implementation that also decodes them, and is what the conformance suite
// holds every generated runtime to.
func (r *Route) MatchPath(segs []string, verb string) (map[string]string, bool) {
	if !r.matches(segs, verb) {
		return nil, false
	}
	if len(r.Captures) == 0 {
		return map[string]string{}, true
	}
	out := make(map[string]string, len(r.Captures))
	for _, c := range r.Captures {
		end := c.End
		if end == ToEnd {
			end = len(segs)
		}
		out[c.Name()] = strings.Join(segs[c.Start:end], "/")
	}
	return out, true
}

// matches is the positional walk both MatchPath and Match share. It decides
// only whether the shape fits; slicing and decoding are the callers' concern.
func (r *Route) matches(segs []string, verb string) bool {
	if verb != r.Verb {
		return false
	}

	n := r.fixed()
	if r.HasMulti() {
		// "**" matches zero or more, so the path may be shorter than the route
		// is long, but never shorter than the fixed prefix.
		if len(segs) < n {
			return false
		}
	} else if len(segs) != n {
		return false
	}

	for i := 0; i < n; i++ {
		m := r.Segments[i]
		switch m.Kind {
		case KindLiteral:
			if segs[i] != m.Literal {
				return false
			}
		case KindSingle:
			// A "*" binds exactly one component, and an empty component — from
			// a doubled or trailing slash — is not one.
			if segs[i] == "" {
				return false
			}
		}
	}
	return true
}
