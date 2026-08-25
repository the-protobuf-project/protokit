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
			// A compound segment binds several ids in one path component,
			// separated by "_", "-", "." or "~". Nothing here supports that,
			// and the naive slice above would take {a}~{b} as one variable
			// literally named "a}~{b" — a silently wrong parse rather than a
			// refusal. Reject it until a fixture needs the feature.
			if strings.ContainsAny(inner, "{}") {
				return nil, fmt.Errorf(
					"resource pattern %q has a compound segment at position %d; "+
						"several variables in one segment are not supported",
					s, i,
				)
			}
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
