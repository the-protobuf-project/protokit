package introspect

// sdl.go parses a GraphQL SDL schema document (schema.graphql) into the same Schema
// shape the introspection JSON decodes to, so a cached schema can be authored as
// human-readable SDL rather than a JSON introspection dump. SDL descriptions (the
// "…" / """…""" strings before a type or field) carry through as the IR's
// descriptions, which the renderer emits as Go doc comments.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// ParseSDL parses GraphQL SDL into a Schema. name labels the source in parse errors.
func ParseSDL(name, src string) (*Schema, error) {
	s, err := gqlparser.LoadSchema(&ast.Source{Name: name, Input: src})
	if err != nil {
		// Real-world SDL routinely applies directives it never declares
		// (federation's @key, Prisma's @unique, engine-specific ones). Declare a
		// permissive stub for each and retry, so the directives survive as data.
		stubs, ok := directiveStubs(name, src)
		if !ok {
			return nil, fmt.Errorf("parse SDL %q: %w", name, err)
		}
		s, err = gqlparser.LoadSchema(&ast.Source{Name: name, Input: src}, &ast.Source{Name: "protokit-directive-stubs.graphql", Input: stubs})
		if err != nil {
			return nil, fmt.Errorf("parse SDL %q: %w", name, err)
		}
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
		if n == stubScalar {
			continue
		}
		ft := sdlFullType(s.Types[n])
		ft.PossibleTypes = sdlPossibleTypes(s, s.Types[n])
		out.Types = append(out.Types, ft)
	}
	return out, nil
}

func sdlFullType(d *ast.Definition) FullType {
	ft := FullType{Kind: sdlKind(d.Kind), Name: d.Name, Description: d.Description, Directives: sdlDirectives(d.Directives)}
	for _, i := range d.Interfaces {
		ft.Interfaces = append(ft.Interfaces, TypeRef{Kind: "INTERFACE", Name: i})
	}
	if sb := d.Directives.ForName("specifiedBy"); sb != nil {
		if a := sb.Arguments.ForName("url"); a != nil && a.Value != nil {
			ft.SpecifiedByURL = a.Value.Raw
		}
	}
	switch d.Kind {
	case ast.Object, ast.Interface:
		for _, f := range d.Fields {
			dep, reason := sdlDeprecated(f.Directives)
			ft.Fields = append(ft.Fields, Field{
				Name:              f.Name,
				Description:       f.Description,
				Args:              sdlArgs(f.Arguments),
				Type:              sdlType(f.Type),
				IsDeprecated:      dep,
				DeprecationReason: reason,
				Directives:        sdlDirectives(f.Directives),
			})
		}
	case ast.InputObject:
		for _, f := range d.Fields {
			ft.InputFields = append(ft.InputFields, InputValue{
				Name:         f.Name,
				Description:  f.Description,
				Type:         sdlType(f.Type),
				DefaultValue: sdlDefault(f.DefaultValue),
				Directives:   sdlDirectives(f.Directives),
			})
		}
	case ast.Enum:
		for _, v := range d.EnumValues {
			dep, reason := sdlDeprecated(v.Directives)
			ft.EnumValues = append(ft.EnumValues, EnumValue{Name: v.Name, Description: v.Description, IsDeprecated: dep, DeprecationReason: reason})
		}
	}
	return ft
}

func sdlArgs(in ast.ArgumentDefinitionList) []InputValue {
	out := make([]InputValue, 0, len(in))
	for _, a := range in {
		out = append(out, InputValue{Name: a.Name, Description: a.Description, Type: sdlType(a.Type), DefaultValue: sdlDefault(a.DefaultValue), Directives: sdlDirectives(a.Directives)})
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

// sdlPossibleTypes fills union members and interface implementations, which
// gqlparser keeps on the schema rather than on the definition.
func sdlPossibleTypes(s *ast.Schema, d *ast.Definition) []TypeRef {
	var out []TypeRef
	switch d.Kind {
	case ast.Union:
		for _, t := range d.Types {
			out = append(out, TypeRef{Kind: "OBJECT", Name: t})
		}
	case ast.Interface:
		for _, impl := range s.GetPossibleTypes(d) {
			if impl.Kind != ast.Object {
				continue // an interface implementing this one is not a runtime type
			}
			out = append(out, TypeRef{Kind: "OBJECT", Name: impl.Name})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	}
	return out
}

func sdlDeprecated(ds ast.DirectiveList) (bool, *string) {
	d := ds.ForName("deprecated")
	if d == nil {
		return false, nil
	}
	reason := "No longer supported"
	if a := d.Arguments.ForName("reason"); a != nil && a.Value != nil {
		reason = a.Value.Raw
	}
	return true, &reason
}

func sdlDefault(v *ast.Value) *string {
	if v == nil {
		return nil
	}
	s := v.String()
	return &s
}

func sdlDirectives(ds ast.DirectiveList) []AppliedDirective {
	var out []AppliedDirective
	for _, d := range ds {
		if d.Name == "deprecated" || d.Name == "specifiedBy" {
			continue
		}
		ad := AppliedDirective{Name: d.Name}
		for _, a := range d.Arguments {
			if ad.Args == nil {
				ad.Args = map[string]string{}
			}
			if a.Value != nil {
				ad.Args[a.Name] = a.Value.String()
			}
		}
		out = append(out, ad)
	}
	return out
}

// stubScalar types every argument of a stubbed directive, accepting any literal.
const stubScalar = "ProtokitAnyLiteral_"

// directiveStubs scans src for applied directives that are neither built in nor
// declared, and returns SDL declaring each one (with every argument name seen
// in use) on all type-system locations.
func directiveStubs(name, src string) (string, bool) {
	doc, err := parser.ParseSchema(&ast.Source{Name: name, Input: src})
	if err != nil {
		return "", false
	}
	declared := map[string]bool{"deprecated": true, "specifiedBy": true, "skip": true, "include": true, "oneOf": true, "defer": true, "stream": true}
	for _, d := range doc.Directives {
		declared[d.Name] = true
	}
	used := map[string]map[string]bool{}
	note := func(ds ast.DirectiveList) {
		for _, d := range ds {
			if declared[d.Name] {
				continue
			}
			if used[d.Name] == nil {
				used[d.Name] = map[string]bool{}
			}
			for _, a := range d.Arguments {
				used[d.Name][a.Name] = true
			}
		}
	}
	defs := append(append(ast.DefinitionList{}, doc.Definitions...), doc.Extensions...)
	for _, d := range defs {
		note(d.Directives)
		for _, f := range d.Fields {
			note(f.Directives)
			for _, a := range f.Arguments {
				note(a.Directives)
			}
		}
		for _, v := range d.EnumValues {
			note(v.Directives)
		}
	}
	for _, sd := range append(doc.Schema, doc.SchemaExtension...) {
		note(sd.Directives)
	}
	if len(used) == 0 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "scalar %s\n", stubScalar)
	dn := make([]string, 0, len(used))
	for n := range used {
		dn = append(dn, n)
	}
	sort.Strings(dn)
	for _, n := range dn {
		fmt.Fprintf(&b, "directive @%s", n)
		if len(used[n]) > 0 {
			an := make([]string, 0, len(used[n]))
			for a := range used[n] {
				an = append(an, a+": "+stubScalar)
			}
			sort.Strings(an)
			fmt.Fprintf(&b, "(%s)", strings.Join(an, ", "))
		}
		b.WriteString(" repeatable on SCHEMA | SCALAR | OBJECT | FIELD_DEFINITION | ARGUMENT_DEFINITION | INTERFACE | UNION | ENUM | ENUM_VALUE | INPUT_OBJECT | INPUT_FIELD_DEFINITION\n")
	}
	return b.String(), true
}
