// Package spec normalizes a GraphQL schema (introspection JSON or SDL, via the
// introspect package) into protokit's source-neutral apispec model.
//
// Unlike the ir package — which shapes a schema for a typed client and drops
// what a client does not need — this keeps everything the schema states:
// interfaces and unions with their members, deprecation, argument defaults,
// applied directives (SDL), custom scalars with their specifiedBy URL, and
// non-null-ness on every field, argument and list element. Engine plumbing
// (Hasura filters, aggregates, mutation envelopes) is kept but marked
// Generated, so consumers choose whether to see it.
package spec

import (
	"strings"

	"github.com/the-protobuf-project/protokit/apispec"
	"github.com/the-protobuf-project/protokit/graphql/dialect"
	"github.com/the-protobuf-project/protokit/graphql/introspect"
	"github.com/the-protobuf-project/protokit/graphql/ir"
)

// Build converts an introspected schema. d supplies engine conventions; nil
// means a hand-written schema (nothing is marked Generated). Use
// dialect.Detect to choose automatically.
func Build(in *introspect.Schema, d dialect.Dialect) *apispec.Spec {
	grouping := d
	if grouping == nil {
		grouping = dialect.Default() // family grouping only needs verb conventions
	}
	s := apispec.New(apispec.SourceGraphQL)
	s.SourceVersion = "graphql"
	kinds := map[string]string{}
	for i := range in.Types {
		kinds[in.Types[i].Name] = in.Types[i].Kind
	}
	b := &builder{s: s, kinds: kinds}

	roots := map[string]apispec.OpKind{}
	if in.QueryType != nil {
		roots[in.QueryType.Name] = apispec.OpQuery
	}
	if in.MutationType != nil {
		roots[in.MutationType.Name] = apispec.OpMutation
	}
	if in.SubscriptionType != nil {
		roots[in.SubscriptionType.Name] = apispec.OpSubscription
	}

	for i := range in.Types {
		t := &in.Types[i]
		if strings.HasPrefix(t.Name, "__") || roots[t.Name] != "" {
			continue
		}
		if t.Kind == "SCALAR" && apispec.ScalarFromName(t.Name) != apispec.ScalarNone && isBuiltin(t.Name) {
			continue
		}
		at := b.typ(t)
		at.Generated = dialect.IsGenerated(d, t.Name, t.Kind)
		s.Types[t.Name] = at
	}

	// Families (resource groupings) from the ir builder become groups.
	family := map[string]string{}
	for _, r := range ir.Build(in, grouping).Resources {
		if t := s.Types[r.Name]; t != nil && t.Generated {
			continue
		}
		s.AddGroup(&apispec.Group{Name: r.Name})
		for _, ops := range [][]*ir.Operation{r.Queries, r.Mutations, r.Subscriptions} {
			for _, op := range ops {
				family[op.Kind+"/"+op.Name] = r.Name
			}
		}
	}

	for i := range in.Types {
		t := &in.Types[i]
		kind, ok := roots[t.Name]
		if !ok {
			continue
		}
		for _, f := range t.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			ref, nonNull := b.ref(f.Type)
			op := &apispec.Operation{
				ID: f.Name, Kind: kind, Description: f.Description,
				Deprecated: f.IsDeprecated, Result: &ref, ResultNullable: !nonNull,
			}
			for _, a := range f.Args {
				op.Params = append(op.Params, b.param(a))
			}
			if g := family[string(kind)+"/"+f.Name]; g != "" {
				op.Groups = []string{g}
			}
			op.Pagination = apispec.DetectPagination(s, op)
			s.Operations = append(s.Operations, op)
		}
	}
	apispec.AssignGroups(s)
	return s
}

func isBuiltin(n string) bool {
	switch n {
	case "String", "Int", "Float", "Boolean", "ID":
		return true
	}
	return false
}

type builder struct {
	s     *apispec.Spec
	kinds map[string]string
}

func (b *builder) typ(t *introspect.FullType) *apispec.Type {
	out := &apispec.Type{Name: t.Name, Description: t.Description, Directives: directives(t.Directives)}
	for _, i := range t.Interfaces {
		out.Implements = append(out.Implements, i.Name)
	}
	switch t.Kind {
	case "OBJECT", "INTERFACE":
		out.Kind = apispec.KindObject
		if t.Kind == "INTERFACE" {
			out.Kind = apispec.KindUnion
		}
		for _, f := range t.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			out.Fields = append(out.Fields, b.field(f))
		}
		for _, p := range t.PossibleTypes {
			out.Variants = append(out.Variants, p.Name)
		}
	case "INPUT_OBJECT":
		out.Kind = apispec.KindInput
		for _, f := range t.InputFields {
			p := b.param(f)
			out.Fields = append(out.Fields, &apispec.Field{
				Name: p.Name, Description: p.Description, Type: p.Type,
				Required: p.Required, Nullable: !p.Required, Default: p.Default,
				Directives: directives(f.Directives),
			})
		}
	case "ENUM":
		out.Kind = apispec.KindEnum
		for _, v := range t.EnumValues {
			out.Values = append(out.Values, &apispec.EnumValue{Value: v.Name, Name: v.Name, Description: v.Description, Deprecated: v.IsDeprecated})
		}
	case "UNION":
		out.Kind = apispec.KindUnion
		for _, p := range t.PossibleTypes {
			out.Variants = append(out.Variants, p.Name)
		}
	case "SCALAR":
		out.Kind = apispec.KindScalar
		sc := apispec.ScalarFromName(t.Name)
		if sc == apispec.ScalarNone {
			sc = apispec.ScalarString
		}
		out.Scalar = &apispec.TypeRef{Scalar: sc, Format: t.Name}
		if t.SpecifiedByURL != "" {
			out.Extensions = map[string]any{"specifiedByURL": t.SpecifiedByURL}
		}
	}
	return out
}

func (b *builder) field(f introspect.Field) *apispec.Field {
	ref, nonNull := b.ref(f.Type)
	out := &apispec.Field{
		Name: f.Name, Description: f.Description, Type: ref,
		Required: nonNull, Nullable: !nonNull,
		Deprecated: f.IsDeprecated, Directives: directives(f.Directives),
	}
	if f.DeprecationReason != nil {
		out.DeprecationReason = *f.DeprecationReason
	}
	for _, a := range f.Args {
		out.Args = append(out.Args, b.param(a))
	}
	return out
}

func (b *builder) param(a introspect.InputValue) *apispec.Param {
	ref, nonNull := b.ref(a.Type)
	p := &apispec.Param{Name: a.Name, In: apispec.InArg, Description: a.Description, Type: ref, Required: nonNull && a.DefaultValue == nil}
	if a.DefaultValue != nil {
		p.Default = *a.DefaultValue
	}
	return p
}

// ref flattens a NON_NULL/LIST chain into a TypeRef, returning whether the
// outermost type is non-null.
func (b *builder) ref(t introspect.TypeRef) (apispec.TypeRef, bool) {
	nonNull := false
	if t.Kind == "NON_NULL" && t.OfType != nil {
		nonNull = true
		t = *t.OfType
	}
	if t.Kind == "LIST" && t.OfType != nil {
		elem, elemNonNull := b.ref(*t.OfType)
		return apispec.TypeRef{List: &elem, ElemNullable: !elemNonNull}, nonNull
	}
	name := t.Name
	if isBuiltin(name) {
		return apispec.TypeRef{Scalar: apispec.ScalarFromName(name), Format: name}, nonNull
	}
	if b.kinds[name] == "SCALAR" {
		sc := apispec.ScalarFromName(name)
		if sc == apispec.ScalarNone {
			// Unknown custom scalar: keep it as a named alias so the consumer
			// sees the name and can map it.
			return apispec.TypeRef{Named: name}, nonNull
		}
		return apispec.TypeRef{Scalar: sc, Format: name}, nonNull
	}
	return apispec.TypeRef{Named: name}, nonNull
}

func directives(ds []introspect.AppliedDirective) []apispec.Directive {
	var out []apispec.Directive
	for _, d := range ds {
		out = append(out, apispec.Directive{Name: d.Name, Args: d.Args})
	}
	return out
}
