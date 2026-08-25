package service

// resource_build.go materializes google.api.resource descriptors into the IR.

import "sort"

// resource materializes a google.api.resource descriptor, memoized by type.
func (b *builder) resource(rd interface {
	GetType() string
	GetPattern() []string
	GetNameField() string
	GetSingular() string
	GetPlural() string
}, message string) *Resource {
	if existing, ok := b.resources[rd.GetType()]; ok {
		return existing
	}

	resource := &Resource{
		Type:      rd.GetType(),
		NameField: rd.GetNameField(),
		Singular:  rd.GetSingular(),
		Plural:    rd.GetPlural(),
		Message:   message,
	}
	if resource.NameField == "" {
		resource.NameField = "name"
	}

	for _, raw := range rd.GetPattern() {
		pattern, err := ParsePattern(raw)
		if err != nil {
			b.diags = append(b.diags, Diagnostic{
				Rule:    "aip",
				Subject: rd.GetType(),
				Message: err.Error(),
			})
			continue
		}
		resource.Patterns = append(resource.Patterns, pattern)
	}

	b.resources[rd.GetType()] = resource
	return resource
}

// ResourceTypes returns the resource types in the IR, ordered parent-first by
// AIP-123 pattern depth so a reader meets a shelf before the books on it.
func (ir *IR) ResourceTypes() []string {
	types := make([]string, 0, len(ir.Resources))
	for name := range ir.Resources {
		types = append(types, name)
	}
	sort.Slice(types, func(i, j int) bool {
		a, b := ir.Resources[types[i]], ir.Resources[types[j]]
		if a.Depth() != b.Depth() {
			return a.Depth() < b.Depth()
		}
		return types[i] < types[j]
	})
	return types
}
