package introspect

// sdl.go parses a GraphQL SDL schema document (schema.graphql) into the same Schema
// shape the introspection JSON decodes to, so a cached schema can be authored as
// human-readable SDL rather than a JSON introspection dump. SDL descriptions (the
// "…" / """…""" strings before a type or field) carry through as the IR's
// descriptions, which the renderer emits as Go doc comments.

import (
	"fmt"
	"sort"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// ParseSDL parses GraphQL SDL into a Schema. name labels the source in parse errors.
func ParseSDL(name, src string) (*Schema, error) {
	s, err := gqlparser.LoadSchema(&ast.Source{Name: name, Input: src})
	if err != nil {
		return nil, fmt.Errorf("parse SDL %q: %w", name, err)
	}

	out := &Schema{}
	if s.Query != nil {
		out.QueryType = &TypeName{Name: s.Query.Name}
	}
	if s.Mutation != nil {
		out.MutationType = &TypeName{Name: s.Mutation.Name}
	}
	if s.Subscription != nil {
		out.SubscriptionType = &TypeName{Name: s.Subscription.Name}
	}

	// Emit types in a deterministic order; classification is by name, so the order
	// only affects the schema.json dump, not generated output.
	names := make([]string, 0, len(s.Types))
	for n := range s.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Types = append(out.Types, sdlFullType(s.Types[n]))
	}
	return out, nil
}

func sdlFullType(d *ast.Definition) FullType {
	ft := FullType{Kind: sdlKind(d.Kind), Name: d.Name, Description: d.Description}
	switch d.Kind {
	case ast.Object, ast.Interface:
		for _, f := range d.Fields {
			ft.Fields = append(ft.Fields, Field{
				Name:        f.Name,
				Description: f.Description,
				Args:        sdlArgs(f.Arguments),
				Type:        sdlType(f.Type),
			})
		}
	case ast.InputObject:
		for _, f := range d.Fields {
			ft.InputFields = append(ft.InputFields, InputValue{
				Name:        f.Name,
				Description: f.Description,
				Type:        sdlType(f.Type),
			})
		}
	case ast.Enum:
		for _, v := range d.EnumValues {
			ft.EnumValues = append(ft.EnumValues, EnumValue{Name: v.Name, Description: v.Description})
		}
	}
	return ft
}

func sdlArgs(in ast.ArgumentDefinitionList) []InputValue {
	out := make([]InputValue, 0, len(in))
	for _, a := range in {
		out = append(out, InputValue{Name: a.Name, Description: a.Description, Type: sdlType(a.Type)})
	}
	return out
}

// sdlType maps a gqlparser type reference to the introspection TypeRef shape (a
// NON_NULL/LIST wrapper chain bottoming out at a named type). The IR only reads the
// wrapper kinds and the innermost name, not the leaf kind, so named leaves are tagged
// generically.
func sdlType(t *ast.Type) TypeRef {
	if t == nil {
		return TypeRef{}
	}
	if t.NonNull {
		inner := sdlType(&ast.Type{NamedType: t.NamedType, Elem: t.Elem})
		return TypeRef{Kind: "NON_NULL", OfType: &inner}
	}
	if t.Elem != nil {
		inner := sdlType(t.Elem)
		return TypeRef{Kind: "LIST", OfType: &inner}
	}
	return TypeRef{Kind: "SCALAR", Name: t.NamedType}
}

func sdlKind(k ast.DefinitionKind) string {
	switch k {
	case ast.Object:
		return "OBJECT"
	case ast.InputObject:
		return "INPUT_OBJECT"
	case ast.Enum:
		return "ENUM"
	case ast.Scalar:
		return "SCALAR"
	case ast.Interface:
		return "INTERFACE"
	case ast.Union:
		return "UNION"
	}
	return string(k)
}
