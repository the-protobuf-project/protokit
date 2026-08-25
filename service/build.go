package service

// build.go is the entry point: descriptors in, IR out.

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Options configures a build.
type Options struct {
	// Domain is the API's error domain, stamped into every AIP-193 ErrorInfo.
	// It is not derivable from the protos, which declare no such thing.
	Domain string

	// Strict is the per-rule severity spec for recoverable problems, matching
	// protokit.Options.Strict: "" warns on everything, "true" makes every rule
	// an error, "route:error,aip:warn" sets severity per rule.
	Strict string
}

// builder carries the state one build accumulates.
type builder struct {
	opts      Options
	messages  map[string]*Message
	enums     map[string]*Enum
	resources map[string]*Resource
	diags     []Diagnostic
}

// Build produces the service IR for every generate-flagged file.
//
// Only generate-flagged files contribute services: an imported file's methods
// belong to the module that owns them, and exposing them here would put a
// dependency's API into a gateway that never asked for it. Messages reached
// from those methods are materialized wherever they live, since a request type
// may perfectly well be imported.
func Build(plugin *protogen.Plugin, opts Options) (*IR, error) {
	b := &builder{
		opts:      opts,
		messages:  map[string]*Message{},
		enums:     map[string]*Enum{},
		resources: map[string]*Resource{},
	}

	ir := &IR{
		Domain:    opts.Domain,
		Messages:  b.messages,
		Enums:     b.enums,
		Resources: b.resources,
	}

	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}
		services := file.Desc.Services()
		for i := range services.Len() {
			svc, err := b.service(services.Get(i), file.Desc.Path())
			if err != nil {
				return nil, err
			}
			ir.Services = append(ir.Services, svc)
		}
	}

	if err := b.checkRouteConflicts(ir); err != nil {
		return nil, err
	}
	ir.Diags = b.diags
	return ir, nil
}

// service builds one service and its methods.
func (b *builder) service(sd protoreflect.ServiceDescriptor, path string) (*Service, error) {
	svc := &Service{
		FullName: string(sd.FullName()),
		Package:  string(sd.ParentFile().Package()),
		Name:     string(sd.Name()),
		File:     path,
		Host:     defaultHost(sd),
		Scopes:   oauthScopes(sd),
	}

	methods := sd.Methods()
	for i := range methods.Len() {
		method, err := b.method(methods.Get(i), svc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", methods.Get(i).FullName(), err)
		}
		svc.Methods = append(svc.Methods, method)
	}
	return svc, nil
}

// method builds one method and every binding declared over it.
//
// The owning service is passed in rather than looked up: response derivation
// needs its oauth_scopes, and the method is not in svc.Methods yet.
func (b *builder) method(md protoreflect.MethodDescriptor, svc *Service) (*Method, error) {
	method := &Method{
		FullName:     string(md.FullName()),
		Name:         string(md.Name()),
		Input:        b.message(md.Input()),
		Output:       b.message(md.Output()),
		ClientStream: md.IsStreamingClient(),
		ServerStream: md.IsStreamingServer(),
		Signatures:   methodSignatures(md),
	}

	rule := httpRule(md)
	if rule == nil {
		// Legal, and simply means the method is not exposed over HTTP.
		method.Pattern = classifyMethod(md, nil)
		method.Mutating = mutating(method.Pattern, nil)
		return method, nil
	}

	// HTTP has no honest mapping for a client-streaming request: there is no
	// way to transcode a sequence of messages into one body without buffering
	// the whole thing, which changes the semantics the author asked for.
	if md.IsStreamingClient() {
		return nil, fmt.Errorf(
			"client streaming cannot be transcoded; remove the google.api.http rule or the stream keyword",
		)
	}

	raws, err := flattenRule(rule)
	if err != nil {
		return nil, err
	}

	for i, raw := range raws {
		source := fmt.Sprintf("%s binding %d", md.FullName(), i)
		binding, err := b.buildBinding(i, raw, method.Input, source)
		if err != nil {
			return nil, fmt.Errorf("binding %d (%s %s): %w", i, raw.httpMethod, raw.template, err)
		}
		binding.responseMessage = method.Output
		if err := b.bindResponseBody(binding, raw); err != nil {
			return nil, fmt.Errorf("binding %d: %w", i, err)
		}
		method.Bindings = append(method.Bindings, binding)
	}

	method.Pattern = classifyMethod(md, raws[0])
	method.Mutating = mutating(method.Pattern, method.Bindings)
	for _, binding := range method.Bindings {
		binding.Responses = b.responsesFor(svc, method, binding)
	}
	return method, nil
}
