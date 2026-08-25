package service

// model.go is the root of the service IR: the whole build, the services in it,
// and the methods they declare.

// IR is one build's service-level intermediate representation.
type IR struct {
	// Domain is the API's error domain, used for the ErrorInfo every AIP-193
	// error carries. It comes from the plugin's configuration rather than from
	// the protos, which do not declare one.
	Domain string

	// Services holds every service in the generate-flagged files, in file and
	// declaration order.
	Services []*Service

	// Resources indexes every AIP-123 resource by its type name, e.g.
	// "library.example.com/Book". Resources declared on messages in imported
	// files are included, because a request message in a generated file can
	// reference one.
	Resources map[string]*Resource

	// Messages indexes every message reachable from a bound method's input or
	// output, by full proto name.
	Messages map[string]*Message

	// Enums indexes every enum reachable the same way.
	Enums map[string]*Enum

	// Diags collects recoverable problems. Severity is resolved against the
	// plugin's strictness setting, matching protokit.Options.Strict.
	Diags []Diagnostic
}

// Service is one gRPC service and its HTTP surface.
type Service struct {
	FullName string // "library.v1.LibraryService"
	Package  string // "library.v1"
	Name     string // "LibraryService"
	File     string // the .proto path it was declared in
	Doc      string

	// Host is google.api.default_host, when declared.
	Host string

	// Scopes is google.api.oauth_scopes, split on commas. Its presence is what
	// tells the OpenAPI target to emit a security scheme and to document 401
	// and 403 on every operation.
	Scopes []string

	Methods []*Method
}

// Method is one RPC, with every HTTP binding declared over it.
type Method struct {
	FullName string // "library.v1.LibraryService.GetBook"
	Name     string // "GetBook"
	Doc      string

	Input  *Message
	Output *Message

	ClientStream bool
	ServerStream bool

	// Pattern classifies the method against the AIP standard methods. It drives
	// middleware selectors, OpenAPI summaries, and the response set a binding
	// advertises.
	Pattern MethodPattern

	// Mutating is derived from Pattern: true for anything that creates,
	// changes, or removes state. A selector written against this stays correct
	// when a method is added, which a name pattern would not.
	Mutating bool

	// Bindings holds the primary google.api.http rule first, then each
	// additional_bindings entry in declaration order. Empty for a method with
	// no rule, which is legal and simply means it is not exposed over HTTP.
	Bindings []*Binding

	// Signatures is google.api.method_signature: each entry is one flattened
	// argument list. Used for client generation and for documentation, not for
	// routing.
	Signatures [][]string

	// Routing is google.api.routing, projected to x-goog-request-params.
	Routing []RoutingParam

	// LRO is set when the method returns google.longrunning.Operation.
	LRO *LROInfo
}
