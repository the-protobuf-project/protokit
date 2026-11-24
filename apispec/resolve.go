package apispec

import "strings"

// ResultResource returns the named type an operation's result denotes once
// lists, connections (edges/nodes), and single-field payload envelopes
// ({user: User}, {data: User}, IssuePayload{issue}) are looked through. isEntity
// reports whether a type name is an entity worth returning; the first entity
// reached wins. It returns "" when no entity is found.
func (s *Spec) ResultResource(op *Operation, isEntity func(string) bool) string {
	if op.Result == nil {
		return ""
	}
	if op.Pagination != nil && op.Pagination.ItemType != "" && isEntity(op.Pagination.ItemType) {
		return op.Pagination.ItemType
	}
	return s.entityIn(*op.Result, isEntity, 0)
}

func (s *Spec) entityIn(r TypeRef, isEntity func(string) bool, depth int) string {
	l := r.Leaf()
	if l.Named == "" || depth > 3 {
		return ""
	}
	if isEntity(l.Named) {
		return l.Named
	}
	t := s.Resolve(l.Named)
	if t == nil || t.Kind != KindObject {
		return ""
	}
	if t.Name != l.Named && isEntity(t.Name) {
		return t.Name
	}
	// Prefer a field named like an entity (issue: Issue) or nodes/edges.
	var found string
	for _, f := range t.Fields {
		fl := f.Type.Leaf().Named
		if fl == "" {
			continue
		}
		if isEntity(fl) && strings.EqualFold(f.Name, fl) {
			return fl
		}
		if found == "" {
			switch strings.ToLower(f.Name) {
			case "nodes", "edges", "node", "data", "items", "result", "returning", "entity":
				found = s.entityIn(f.Type, isEntity, depth+1)
			}
		}
	}
	if found != "" {
		return found
	}
	for _, f := range t.Fields {
		if n := f.Type.Leaf().Named; n != "" && isEntity(n) && len(t.Fields) <= 4 {
			return n
		}
	}
	return ""
}
