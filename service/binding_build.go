package service

// binding_build.go turns one google.api.http rule into a Binding: the template
// is parsed and compiled, the body spec resolved, and the query parameters
// computed by subtraction.

import (
	"fmt"

	"google.golang.org/genproto/googleapis/api/annotations"

	"github.com/the-protobuf-project/protokit/service/httprule"
)

// httpBinding is one rule, flattened out of the oneof google.api.http uses.
type httpBinding struct {
	httpMethod   string
	template     string
	body         string
	responseBody string
	verb         string
}

// flattenRule returns the primary rule and each additional_bindings entry, in
// declaration order, with the oneof resolved.
//
// additional_bindings may not nest, so this does not recurse: a nested entry is
// invalid and reported by the caller rather than silently flattened.
func flattenRule(rule *annotations.HttpRule) ([]*httpBinding, error) {
	if rule == nil {
		return nil, nil
	}
	out := make([]*httpBinding, 0, 1+len(rule.GetAdditionalBindings()))

	primary, err := oneRule(rule)
	if err != nil {
		return nil, err
	}
	out = append(out, primary)

	for i, extra := range rule.GetAdditionalBindings() {
		if len(extra.GetAdditionalBindings()) > 0 {
			return nil, fmt.Errorf("additional_bindings[%d] declares its own additional_bindings, which is not allowed", i)
		}
		binding, err := oneRule(extra)
		if err != nil {
			return nil, fmt.Errorf("additional_bindings[%d]: %w", i, err)
		}
		out = append(out, binding)
	}
	return out, nil
}

// oneRule resolves the pattern oneof into a method and a template.
func oneRule(rule *annotations.HttpRule) (*httpBinding, error) {
	binding := &httpBinding{
		body:         rule.GetBody(),
		responseBody: rule.GetResponseBody(),
	}

	switch pattern := rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		binding.httpMethod, binding.template = "GET", pattern.Get
	case *annotations.HttpRule_Put:
		binding.httpMethod, binding.template = "PUT", pattern.Put
	case *annotations.HttpRule_Post:
		binding.httpMethod, binding.template = "POST", pattern.Post
	case *annotations.HttpRule_Delete:
		binding.httpMethod, binding.template = "DELETE", pattern.Delete
	case *annotations.HttpRule_Patch:
		binding.httpMethod, binding.template = "PATCH", pattern.Patch
	case *annotations.HttpRule_Custom:
		// The escape hatch, for a method HTTP has no verb for.
		binding.httpMethod, binding.template = pattern.Custom.GetKind(), pattern.Custom.GetPath()
	default:
		return nil, fmt.Errorf("the rule declares no pattern (get, post, put, patch, delete, or custom)")
	}

	if binding.template == "" {
		return nil, fmt.Errorf("the %s pattern has an empty path", binding.httpMethod)
	}
	return binding, nil
}

// buildBinding compiles one rule against its request message.
func (b *builder) buildBinding(
	index int,
	raw *httpBinding,
	input *Message,
	source string,
) (*Binding, error) {
	tmpl, err := httprule.Parse(raw.template)
	if err != nil {
		return nil, err
	}
	route, err := httprule.Compile(tmpl)
	if err != nil {
		return nil, err
	}
	route.HTTPMethod = raw.httpMethod
	route.Source = source

	binding := &Binding{
		Index:      index,
		HTTPMethod: raw.httpMethod,
		Template:   tmpl,
		Route:      route,
		Verb:       tmpl.Verb,
	}
	raw.verb = tmpl.Verb

	if err := b.bindPathParams(binding, input); err != nil {
		return nil, err
	}
	if err := b.bindBody(binding, raw, input); err != nil {
		return nil, err
	}
	if err := b.bindResponseBody(binding, raw); err != nil {
		return nil, err
	}
	b.bindQueryParams(binding, input)
	return binding, nil
}

// bindPathParams resolves each template capture against the request message.
func (b *builder) bindPathParams(binding *Binding, input *Message) error {
	for i := range binding.Route.Captures {
		capture := &binding.Route.Captures[i]

		path, err := b.resolveFieldPath(input, capture.Field)
		if err != nil {
			return fmt.Errorf("path variable %q: %w", capture.Name(), err)
		}
		leaf := path.Leaf()
		if !leaf.Bindable() {
			return fmt.Errorf(
				"path variable %q binds field %q of type %s, which cannot be parsed from a path segment",
				capture.Name(), leaf.Name, leaf.Kind,
			)
		}
		// A repeated field only makes sense under a "**", which is the one
		// capture that can span more than one segment.
		if leaf.Repeated && capture.End != httprule.ToEnd {
			return fmt.Errorf(
				"path variable %q binds repeated field %q, which requires a %q capture",
				capture.Name(), leaf.Name, "**",
			)
		}
		binding.PathParams = append(binding.PathParams, &Param{
			Path:     path,
			Capture:  capture,
			Repeated: leaf.Repeated,
			Required: leaf.Has(BehaviorRequired),
		})
	}
	return nil
}
