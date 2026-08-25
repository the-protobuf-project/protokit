package service

import (
	"fmt"
	"strings"
)

// Resource is an AIP-123 google.api.resource descriptor.
//
// It carries more than validation needs because the OpenAPI target navigates by
// resource: Singular and Plural name the folders a client tool builds, and
// Patterns are what let an opaque {name=shelves/*/books/*} capture expand into
// readable /v1/shelves/{shelf}/books/{book} path parameters.
type Resource struct {
	// Type is the resource type name, e.g. "library.example.com/Book".
	Type string

	// Patterns are the resource-name patterns, in declaration order. The first
	// is canonical; later ones exist for resources addressable more than one
	// way.
	Patterns []*Pattern

	// NameField is the field holding the resource name, defaulting to "name".
	NameField string

	// Singular and Plural are the AIP-123 names, e.g. "book" and "books".
	// Plural, title-cased, is the OpenAPI tag.
	Singular string
	Plural   string

	// Message is the full proto name of the message declaring this resource,
	// empty for a resource declared at file scope.
	Message string
}

// Depth returns the number of collection segments in the canonical pattern,
// which orders parents before children: "shelves/{shelf}" is depth 1 and
// "shelves/{shelf}/books/{book}" is depth 2.
//
// The OpenAPI target sorts its root tag list by this, so a reader meets shelves
// before the books that live on them.
func (r *Resource) Depth() int {
	if len(r.Patterns) == 0 {
		return 0
	}
	return len(r.Patterns[0].Vars())
}

// Pattern is a parsed resource-name pattern: "shelves/{shelf}/books/{book}".
type Pattern struct {
	Raw      string
	Segments []PatternSegment
}

// PatternSegment is one component of a pattern: either a fixed collection id or
// a variable standing for one resource id.
type PatternSegment struct {
	// Literal is the collection id, set when Var is empty.
	Literal string

	// Var is the variable name, e.g. "shelf". Empty for a literal segment.
	Var string

	// Multi is true for a trailing {var=**}, which absorbs the remaining
	// segments. AIP discourages it, but google.api.resource permits it and a
	// singleton nested under an arbitrary parent uses it.
	Multi bool
}

// ParsePattern parses a resource-name pattern.
//
// Patterns are simpler than path templates: no leading slash, no custom verb,
// and a variable is always a whole segment.
func ParsePattern(s string) (*Pattern, error) {
	if s == "" {
		return nil, fmt.Errorf("empty resource pattern")
	}
	if strings.HasPrefix(s, "/") {
		return nil, fmt.Errorf("resource pattern %q must not begin with %q: a resource name is relative (AIP-122)", s, "/")
	}

	p := &Pattern{Raw: s}
	parts := strings.Split(s, "/")
	for i, part := range parts {
		switch {
		case part == "":
			return nil, fmt.Errorf("resource pattern %q has an empty segment at position %d", s, i)

		case strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			inner := part[1 : len(part)-1]
			multi := false
			if name, rest, ok := strings.Cut(inner, "="); ok {
				if rest != "**" {
					return nil, fmt.Errorf("resource pattern %q: only %q is allowed after %q in a variable, found %q", s, "**", "=", rest)
				}
				inner, multi = name, true
			}
			if inner == "" {
				return nil, fmt.Errorf("resource pattern %q has an unnamed variable at position %d", s, i)
			}
			if multi && i != len(parts)-1 {
				return nil, fmt.Errorf("resource pattern %q: %q must be the final segment", s, "**")
			}
			p.Segments = append(p.Segments, PatternSegment{Var: inner, Multi: multi})

		default:
			if strings.ContainsAny(part, "{}") {
				return nil, fmt.Errorf("resource pattern %q has a malformed variable at position %d", s, i)
			}
			p.Segments = append(p.Segments, PatternSegment{Literal: part})
		}
	}
	return p, nil
}

// Vars returns the pattern's variable names in order.
func (p *Pattern) Vars() []string {
	var out []string
	for _, s := range p.Segments {
		if s.Var != "" {
			out = append(out, s.Var)
		}
	}
	return out
}

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
		last := p.Segments[len(p.Segments)-1]
		out[last.Var] = strings.Join(segs[fixed:], "/")
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
