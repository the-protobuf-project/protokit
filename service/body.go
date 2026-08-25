package service

// body.go resolves a rule's body: and response_body: specs, and computes the
// query parameters by subtracting what the path and body already bind.

import (
	"fmt"
	"strings"
)

// bindBody resolves the rule's body spec.
func (b *builder) bindBody(binding *Binding, raw *httpBinding, input *Message) error {
	switch raw.body {
	case "":
		// No body. A request that sends one anyway is rejected at runtime; see
		// PROTOCOL.md §4.2.
		return nil

	case "*":
		binding.Body = &BodySpec{Wildcard: true}
		return nil

	default:
		path, err := b.resolveFieldPath(input, strings.Split(raw.body, "."))
		if err != nil {
			return fmt.Errorf("body %q: %w", raw.body, err)
		}
		leaf := path.Leaf()
		if leaf.Repeated {
			return fmt.Errorf("body %q names a repeated field, which cannot receive a request body", raw.body)
		}
		if leaf.Kind != KindMessage {
			return fmt.Errorf("body %q names a field of type %s; a body field must be a message", raw.body, leaf.Kind)
		}
		binding.Body = &BodySpec{
			Field: path,
			// google.api.HttpBody bypasses the codec: the raw bytes and the
			// caller's Content-Type go straight into the message.
			Passthrough: leaf.WellKnown == WKHTTPBody,
		}
		return nil
	}
}

// bindResponseBody resolves the rule's response_body spec.
func (b *builder) bindResponseBody(binding *Binding, raw *httpBinding) error {
	if raw.responseBody == "" {
		return nil
	}
	output := binding.responseMessage
	if output == nil {
		return fmt.Errorf("response_body %q: the response message is not resolvable", raw.responseBody)
	}

	path, err := b.resolveFieldPath(output, strings.Split(raw.responseBody, "."))
	if err != nil {
		return fmt.Errorf("response_body %q: %w", raw.responseBody, err)
	}
	binding.ResponseBody = path
	return nil
}

// bindQueryParams records every input field the path and body do not bind.
//
// The subtraction happens here, once, because neither runtime can do it: prost
// has no reflection and the Python message layer has none either. Go's gateway
// walks fields at request time with a filter of what is already bound; both
// runtimes here receive an explicit list instead.
func (b *builder) bindQueryParams(binding *Binding, input *Message) {
	// A wildcard body claims the whole message, leaving nothing for the query.
	if binding.Body != nil && binding.Body.Wildcard {
		return
	}

	bound := map[string]bool{}
	for _, param := range binding.PathParams {
		bound[param.Path.JSON] = true
	}
	if binding.Body != nil && binding.Body.Field != nil {
		bound[binding.Body.Field.JSON] = true
	}

	b.walkQueryable(input, nil, bound, &binding.QueryParams, 0)
}

// maxQueryDepth bounds the recursion into nested messages.
//
// A self-referential message would otherwise expand forever. Google's own
// gateway uses the same bound for the same reason.
const maxQueryDepth = 5

// walkQueryable collects the bindable leaves under a message.
func (b *builder) walkQueryable(
	msg *Message,
	prefix []*Field,
	bound map[string]bool,
	out *[]*Param,
	depth int,
) {
	if msg == nil || depth > maxQueryDepth {
		return
	}

	for _, field := range msg.Fields {
		path := b.fieldPath(append(append([]*Field{}, prefix...), field))

		// A field the path or body already bound is not also bindable from the
		// query: two sources for one field means one of them silently loses.
		if bound[path.JSON] {
			continue
		}
		// An OUTPUT_ONLY field is the service's to set; accepting it from a
		// caller would let them claim a server-assigned value.
		if field.Has(BehaviorOutputOnly) {
			continue
		}

		switch {
		case field.Bindable():
			*out = append(*out, &Param{
				Path:     path,
				Repeated: field.Repeated,
				Required: field.Has(BehaviorRequired),
			})

		case field.Kind == KindMessage && !field.Repeated:
			// Recurse into a nested message: `?book.displayName=` binds a leaf
			// two levels down.
			b.walkQueryable(b.messages[field.Message], path.Fields, bound, out, depth+1)
		}
		// A map or a repeated message has no query spelling, so it is skipped.
	}
}
