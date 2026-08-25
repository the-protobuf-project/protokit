package service

// resource_match.go matches a resource name against an AIP-123 pattern.

import "strings"

// Match validates a resource name against the pattern and returns the variable
// bindings.
//
// A name is relative and has no leading slash (AIP-122); one that does is
// rejected rather than tolerated, because accepting both spellings means two
// distinct names for one resource.
func (p *Pattern) Match(name string) (map[string]string, bool) {
	if name == "" || strings.HasPrefix(name, "/") {
		return nil, false
	}
	segs := strings.Split(name, "/")

	multi := len(p.Segments) > 0 && p.Segments[len(p.Segments)-1].Multi
	fixed := len(p.Segments)
	if multi {
		fixed--
	}
	if multi {
		if len(segs) < fixed {
			return nil, false
		}
	} else if len(segs) != fixed {
		return nil, false
	}

	out := map[string]string{}
	for i := 0; i < fixed; i++ {
		ps, got := p.Segments[i], segs[i]
		if got == "" {
			return nil, false // an empty id is not an id
		}
		if ps.Var == "" {
			if got != ps.Literal {
				return nil, false
			}
			continue
		}
		out[ps.Var] = got
	}
	if multi {
		rest := segs[fixed:]
		// Zero segments is legal — a trailing "**" may match nothing — but an
		// empty one is not an id, and joining it would bind a name containing
		// "//" that no resource can have.
		for _, seg := range rest {
			if seg == "" {
				return nil, false
			}
		}
		last := p.Segments[len(p.Segments)-1]
		out[last.Var] = strings.Join(rest, "/")
	}
	return out, true
}

// String renders the pattern back to its source form.
func (p *Pattern) String() string {
	parts := make([]string, len(p.Segments))
	for i, s := range p.Segments {
		switch {
		case s.Var != "" && s.Multi:
			parts[i] = "{" + s.Var + "=**}"
		case s.Var != "":
			parts[i] = "{" + s.Var + "}"
		default:
			parts[i] = s.Literal
		}
	}
	return strings.Join(parts, "/")
}
