package apispec

// OpKind is how an operation is invoked.
type OpKind string

const (
	OpQuery        OpKind = "query"        // GraphQL query root field
	OpMutation     OpKind = "mutation"     // GraphQL mutation root field
	OpSubscription OpKind = "subscription" // GraphQL subscription root field
	OpHTTP         OpKind = "http"         // OpenAPI path operation
	OpWebhook      OpKind = "webhook"      // OpenAPI 3.1 webhook / callback (server → client)
)

// Operation is one callable entry point.
type Operation struct {
	ID             string // operationId, or GraphQL root field name
	Kind           OpKind
	Summary        string
	Description    string
	Groups         []string // tags / families
	Deprecated     bool
	HTTP           *HTTPBinding
	Params         []*Param
	Body           *Body
	Result         *TypeRef // primary success payload (GraphQL return type, 2xx schema)
	ResultNullable bool
	Responses      []*Response
	Security       []map[string][]string // alternatives; nil = inherit Spec.DefaultSecurity, empty = explicitly none
	Extensions     map[string]any
	Pagination     *Pagination // detected/declared paging contract, if any
}

// HTTPBinding is an operation's HTTP surface.
type HTTPBinding struct {
	Method string // upper-case: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, TRACE
	Path   string // template as written, e.g. /users/{user_id}/posts
}

// ParamIn is where a parameter travels.
type ParamIn string

const (
	InPath   ParamIn = "path"
	InQuery  ParamIn = "query"
	InHeader ParamIn = "header"
	InCookie ParamIn = "cookie"
	InArg    ParamIn = "arg" // GraphQL argument
)

// Param is one operation input other than the body.
type Param struct {
	Name        string
	In          ParamIn
	Description string
	Type        TypeRef
	Required    bool
	Deprecated  bool
	Default     any
	Example     any
	Constraints Constraints
	Style       string // OpenAPI serialization style; a Swagger 2.0 collectionFormat with no 3.x equivalent is kept verbatim ("tsv")
	Explode     *bool
	Nullable    bool        // may be explicitly null (GraphQL: the type is not non-null, even when a default makes it optional)
	Directives  []Directive // GraphQL directives applied to the argument
}

// Body is a request body.
type Body struct {
	Description  string
	Required     bool
	ContentTypes []string // media types, in source order
	Type         TypeRef
}

// Response is one declared response.
type Response struct {
	Status       string // "200", "2XX", "default"
	Description  string
	ContentTypes []string
	Type         *TypeRef
	Headers      []*Param
}

// Pagination is a paging contract on a list operation.
type Pagination struct {
	Style      string // "cursor", "offset", "page", "relay"
	SizeParam  string // limit / first / page_size
	TokenParam string // cursor / after / page_token / offset / page
	ItemsField string // field in the result holding the items
	NextField  string // field in the result holding the next cursor
	ItemType   string // named type of one item
}

// SecurityScheme is a declared auth mechanism.
type SecurityScheme struct {
	Name         string
	Type         string // apiKey, http, oauth2, openIdConnect, mutualTLS
	Description  string
	In           string // apiKey: header/query/cookie
	ParamName    string // apiKey: header/query name
	Scheme       string // http: bearer/basic
	BearerFormat string
	OpenIDURL    string
	Flows        map[string]*OAuthFlow
}

// OAuthFlow is one OAuth2 flow.
type OAuthFlow struct {
	AuthorizationURL string
	TokenURL         string
	RefreshURL       string
	Scopes           map[string]string
}

// EffectiveSecurity is the security requirement that applies to op: its own
// when it declares one (an empty list meaning explicitly none), otherwise the
// document's default.
func (s *Spec) EffectiveSecurity(op *Operation) []map[string][]string {
	if op.Security != nil {
		return op.Security
	}
	return s.DefaultSecurity
}
