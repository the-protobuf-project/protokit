// Package introspect fetches and models a GraphQL server's introspection response.
//
// It POSTs the standard introspection query to an endpoint and decodes the
// __schema payload into Go structs. These structs mirror the GraphQL introspection
// format closely; normalization into a friendlier shape happens in the ir package.
package introspect

// Response is the top-level introspection HTTP response envelope.
type Response struct {
	Data struct {
		Schema Schema `json:"__schema"`
	} `json:"data"`
	Errors []GraphQLError `json:"errors"`
}

// GraphQLError is a single error entry returned by the server.
type GraphQLError struct {
	Message string `json:"message"`
}

// Schema is the introspected __schema object.
type Schema struct {
	QueryType        *TypeName  `json:"queryType"`
	MutationType     *TypeName  `json:"mutationType"`
	SubscriptionType *TypeName  `json:"subscriptionType"`
	Types            []FullType `json:"types"`
}

// TypeName references a root operation type by name (e.g. "Query").
type TypeName struct {
	Name string `json:"name"`
}

// FullType is a complete type definition: object, input object, enum, scalar, etc.
type FullType struct {
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Fields      []Field      `json:"fields"`
	InputFields []InputValue `json:"inputFields"`
	EnumValues  []EnumValue  `json:"enumValues"`
	Interfaces  []TypeRef    `json:"interfaces"`
	// PossibleTypes lists a union's members or an interface's implementations.
	PossibleTypes []TypeRef `json:"possibleTypes"`
	// SpecifiedByURL is a custom scalar's specification link (GraphQL 2021+).
	SpecifiedByURL string `json:"specifiedByURL,omitempty"`
	// Directives are the directives applied to the definition. Introspection
	// does not expose them; only SDL parsing fills this.
	Directives []AppliedDirective `json:"appliedDirectives,omitempty"`
}

// AppliedDirective is one directive application (@key(fields: "id")), with each
// argument kept as its raw GraphQL literal text.
type AppliedDirective struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args,omitempty"`
}

// Field is a field on an object/interface type, including its arguments.
type Field struct {
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	Args              []InputValue `json:"args"`
	Type              TypeRef      `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason"`
	// Directives: SDL only (see FullType.Directives).
	Directives []AppliedDirective `json:"appliedDirectives,omitempty"`
}

// InputValue is an argument or input-object field with its type and default.
type InputValue struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Type         TypeRef `json:"type"`
	DefaultValue *string `json:"defaultValue"`
	// Directives: SDL only (see FullType.Directives).
	Directives []AppliedDirective `json:"appliedDirectives,omitempty"`
}

// EnumValue is a single value of an enum type.
type EnumValue struct {
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason"`
}

// TypeRef is a (possibly wrapped) reference to a type. Kind is one of NON_NULL,
// LIST, OBJECT, SCALAR, ENUM, INPUT_OBJECT, INTERFACE, UNION. For NON_NULL and LIST,
// OfType holds the wrapped type; the chain bottoms out at a named type.
type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *TypeRef `json:"ofType"`
}
