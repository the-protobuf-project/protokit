package service

// binding.go models one google.api.http rule: what it matches, what it binds,
// and what it can return.

import "github.com/the-protobuf-project/protokit/service/httprule"

// Binding is one google.api.http rule: a method, a path template, an optional
// body, an optional custom verb.
type Binding struct {
	// Index is 0 for the primary rule and 1..n for additional_bindings.
	Index int

	HTTPMethod string // "GET", "POST", "PATCH", "PUT", "DELETE", or a custom token

	// Template is the parsed path template, and Route is the same template
	// flattened for matching. Route is what a target emits; Template is what a
	// diagnostic quotes.
	Template *httprule.Template
	Route    *httprule.Route

	// Verb is the AIP-136 custom-method suffix without its colon, or "".
	Verb string

	// Body describes what the request body decodes into. Nil means the binding
	// accepts no body.
	Body *BodySpec

	// ResponseBody is the field whose value alone forms the response body, from
	// the rule's response_body. Nil means the whole message.
	ResponseBody *FieldPath

	// PathParams is one entry per template capture, resolved against Input.
	PathParams []*Param

	// QueryParams is every Input field the path and body do not bind, computed
	// here so no runtime has to subtract them reflectively.
	QueryParams []*Param

	// Validations is every rule that must hold before the RPC is dialled,
	// gathered from field behaviour, resource patterns, field formats, and CEL.
	Validations []Rule

	// Responses is the status set this binding can produce, for OpenAPI. It
	// always includes the success case.
	Responses []StatusCase

	// responseMessage is the method's output, kept so response_body can be
	// resolved after the binding itself is built.
	responseMessage *Message
}

// BodySpec says what a binding's request body means.
type BodySpec struct {
	// Wildcard is true for body: "*" — the whole message.
	Wildcard bool

	// Field is the target field for a named body. Nil when Wildcard.
	Field *FieldPath

	// Passthrough is true when the target is google.api.HttpBody, in which case
	// no codec is involved: the raw bytes and the Content-Type go straight into
	// the message.
	Passthrough bool
}

// FieldPath is a resolved path into a message, carrying both the proto spelling
// a generator emits setters for and the protojson spelling a client sends.
type FieldPath struct {
	// Proto is the path in proto field names: ["book", "display_name"].
	Proto []string

	// JSON is the same path in protojson names, joined: "book.displayName".
	// This is the name a query parameter uses, the name a FieldViolation
	// reports, and the name OpenAPI documents.
	JSON string

	// Fields is the resolved field at each step, so a generator can emit typed
	// accessors without re-resolving.
	Fields []*Field
}

// Leaf returns the final field of the path.
func (p *FieldPath) Leaf() *Field {
	if len(p.Fields) == 0 {
		return nil
	}
	return p.Fields[len(p.Fields)-1]
}

// Param is one bindable request field, reached from the path or the query.
type Param struct {
	Path *FieldPath

	// Capture is set for a path parameter: the template span that fills it.
	Capture *httprule.Capture

	// Repeated is true when the leaf field is repeated. A repeated path
	// parameter is only legal under a "**" capture; a repeated query parameter
	// takes one occurrence per element.
	Repeated bool

	// Required is true when the leaf carries field_behavior REQUIRED.
	Required bool
}
