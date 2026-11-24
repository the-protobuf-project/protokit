package apispec

import "sort"

// New returns an empty Spec for source.
func New(source Source) *Spec {
	return &Spec{
		Source:   source,
		Types:    map[string]*Type{},
		Security: map[string]*SecurityScheme{},
	}
}

// TypeNames returns the named types sorted by name.
func (s *Spec) TypeNames() []string {
	out := make([]string, 0, len(s.Types))
	for n := range s.Types {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the named type a reference's leaf points at, or nil.
func (s *Spec) Lookup(t TypeRef) *Type {
	l := t.Leaf()
	if l.Named == "" {
		return nil
	}
	return s.Types[l.Named]
}

// Group returns the group named n, or nil.
func (s *Spec) Group(n string) *Group {
	for _, g := range s.Groups {
		if g.Name == n {
			return g
		}
	}
	return nil
}

// AddGroup appends a group unless one with the same name exists.
func (s *Spec) AddGroup(g *Group) {
	if s.Group(g.Name) == nil {
		s.Groups = append(s.Groups, g)
	}
}

// Field returns the field named n, or nil.
func (t *Type) Field(n string) *Field {
	for _, f := range t.Fields {
		if f.Name == n {
			return f
		}
	}
	return nil
}

// References returns the named types this type's fields point at (deduplicated,
// sorted). Union variants and interface parents are included.
func (t *Type) References() []string {
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if n := f.Type.Leaf().Named; n != "" {
			seen[n] = true
		}
	}
	for _, v := range t.Variants {
		seen[v] = true
	}
	if t.Scalar != nil {
		if n := t.Scalar.Leaf().Named; n != "" {
			seen[n] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
