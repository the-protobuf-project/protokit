package apispec

// Source names the format a Spec was built from.
type Source string

const (
	SourceGraphQL Source = "graphql"
	SourceOpenAPI Source = "openapi"
)

// Spec is one normalized API contract.
type Spec struct {
	Source        Source
	SourceVersion string // "3.1.0", "2.0", "graphql" …
	Title         string
	Description   string
	Version       string // the API's own version (info.version)
	Servers       []Server
	Groups        []*Group         // source-declared grouping (tags, families), in source order
	Types         map[string]*Type // named types by name
	Operations    []*Operation     // in source order
	Security      map[string]*SecurityScheme
	Extensions    map[string]any // x-* at the document root
}

// Server is a base URL the API is served from.
type Server struct {
	URL         string
	Description string
}

// Group is a grouping the source itself declares: an OpenAPI tag, or a GraphQL
// type family. Consumers use it as a domain hint, never as a guarantee.
type Group struct {
	Name        string
	Description string
	Parent      string // x-tagGroups / nested tag parent, when declared
}

// Kind classifies a named type.
type Kind string

const (
	KindObject Kind = "object" // fields
	KindInput  Kind = "input"  // GraphQL input object (write-side shape)
	KindEnum   Kind = "enum"
	KindUnion  Kind = "union"  // oneOf / anyOf / GraphQL union or interface
	KindScalar Kind = "scalar" // a named alias of a scalar (custom GraphQL scalar, top-level string schema)
	KindMap    Kind = "map"    // object with only additionalProperties
)

// Type is a named type.
type Type struct {
	Name          string
	Kind          Kind
	Description   string
	Deprecated    bool
	Synthetic     bool     // named by the frontend (inline schema lifted out), not by the source
	Generated     bool     // engine plumbing (filter/order-by/aggregate/mutation-envelope types), not domain model
	Fields        []*Field // object/input
	Values        []*EnumValue
	Variants      []string // union members (named types)
	Discriminator *Discriminator
	Implements    []string // GraphQL interfaces / OpenAPI allOf parents
	Scalar        *TypeRef // KindScalar: what the alias stands for; KindMap: value type in Elem
	Groups        []string // groups (tags) this type was reached from
	Directives    []Directive
	Extensions    map[string]any
	Example       any
}

// Field is one property of an object or input type.
type Field struct {
	Name              string
	Description       string
	Type              TypeRef
	Required          bool // must be present (OpenAPI required, GraphQL non-null)
	Nullable          bool // may be explicitly null
	ReadOnly          bool // server-produced (OpenAPI readOnly)
	WriteOnly         bool // never returned (OpenAPI writeOnly, e.g. password)
	Deprecated        bool
	DeprecationReason string
	Default           any
	Example           any
	Constraints       Constraints
	Args              []*Param // GraphQL field arguments
	Directives        []Directive
	Extensions        map[string]any
}

// EnumValue is one enum member. Value is the wire value ("in_progress", 3);
// Name is a source-provided symbolic name when one exists (x-enum-varnames).
type EnumValue struct {
	Value       any
	Name        string
	Description string
	Deprecated  bool
}

// Discriminator records an OpenAPI discriminator.
type Discriminator struct {
	Property string
	Mapping  map[string]string // wire value -> type name
}

// Directive is a GraphQL directive application (@key(fields: "id")).
type Directive struct {
	Name string
	Args map[string]string // raw GraphQL literal text per argument
}

// TypeRef references a type: a scalar, a named type, or a list/map of one.
type TypeRef struct {
	Scalar       Scalar   // set for a scalar leaf
	Named        string   // set for a reference to a named Type
	List         *TypeRef // set for an array/list; the element type
	Map          *TypeRef // set for a string-keyed map; the value type
	ElemNullable bool     // list/map elements may be null
	Format       string   // the source's own format string, verbatim ("int64", "uuid", "timestamptz")
}

// IsList reports whether the reference is a list.
func (t TypeRef) IsList() bool { return t.List != nil }

// IsMap reports whether the reference is a map.
func (t TypeRef) IsMap() bool { return t.Map != nil }

// Leaf returns the innermost non-container reference.
func (t TypeRef) Leaf() TypeRef {
	for {
		switch {
		case t.List != nil:
			t = *t.List
		case t.Map != nil:
			t = *t.Map
		default:
			return t
		}
	}
}

// Constraints are the validation facts a source states about a value.
type Constraints struct {
	MinLength, MaxLength         *int64
	Pattern                      string
	Minimum, Maximum             *float64
	ExclusiveMinimum             bool
	ExclusiveMaximum             bool
	MultipleOf                   *float64
	MinItems, MaxItems           *int64
	UniqueItems                  bool
	MinProperties, MaxProperties *int64
	Const                        any
}

// IsZero reports whether no constraint is set.
func (c Constraints) IsZero() bool {
	return c.MinLength == nil && c.MaxLength == nil && c.Pattern == "" &&
		c.Minimum == nil && c.Maximum == nil && c.MultipleOf == nil &&
		c.MinItems == nil && c.MaxItems == nil && !c.UniqueItems &&
		c.MinProperties == nil && c.MaxProperties == nil && c.Const == nil
}
