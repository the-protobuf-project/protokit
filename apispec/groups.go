package apispec

// AssignGroups propagates each operation's groups onto every named type it
// reaches (parameters, body, result, responses, transitively through fields),
// so a type used only by "billing" operations carries the "billing" group.
// Existing type groups are kept; order follows Spec.Groups.
func AssignGroups(s *Spec) {
	add := map[string]map[string]bool{}
	for _, op := range s.Operations {
		if len(op.Groups) == 0 {
			continue
		}
		seen := map[string]bool{}
		var visit func(TypeRef)
		visit = func(r TypeRef) {
			n := r.Leaf().Named
			if n == "" || seen[n] {
				return
			}
			seen[n] = true
			t := s.Types[n]
			if t == nil {
				return
			}
			for _, ref := range t.References() {
				visit(TypeRef{Named: ref})
			}
		}
		for _, p := range op.Params {
			visit(p.Type)
		}
		if op.Body != nil {
			visit(op.Body.Type)
		}
		if op.Result != nil {
			visit(*op.Result)
		}
		for _, r := range op.Responses {
			if r.Type != nil {
				visit(*r.Type)
			}
		}
		for n := range seen {
			if add[n] == nil {
				add[n] = map[string]bool{}
			}
			for _, g := range op.Groups {
				add[n][g] = true
			}
		}
	}
	for n, gs := range add {
		t := s.Types[n]
		if t == nil {
			continue // a reference to a type the source never defined
		}
		have := map[string]bool{}
		for _, g := range t.Groups {
			have[g] = true
		}
		for _, g := range s.Groups { // stable: Spec.Groups order
			if gs[g.Name] && !have[g.Name] {
				t.Groups = append(t.Groups, g.Name)
			}
		}
	}
}

// Resolve follows scalar/map aliases: for a KindScalar type whose alias target
// is another named type, it returns that type. Non-alias types return themselves.
func (s *Spec) Resolve(name string) *Type {
	for i := 0; i < 32; i++ {
		t := s.Types[name]
		if t == nil || t.Kind != KindScalar || t.Scalar == nil || t.Scalar.Named == "" || t.Scalar.List != nil {
			return t
		}
		name = t.Scalar.Named
	}
	return s.Types[name]
}
