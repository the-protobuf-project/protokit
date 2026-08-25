// Package service builds a service-level intermediate representation from proto
// descriptors: RPC services, their methods, and the HTTP bindings that
// google.api.http declares over them.
//
// It is the RPC counterpart to protokit's schema IR. Where that one answers
// "what tables does this API imply", this one answers "what HTTP surface does
// this API declare, and what does each request mean" — and it reads the AIP
// vocabulary to do it: google.api.http (AIP-127), google.api.resource
// (AIP-123), google.api.field_behavior (AIP-203), google.api.field_info,
// google.api.routing, google.api.method_signature, and google.longrunning.
//
// Everything that requires understanding protobuf happens here, at build time.
// A generator consuming this IR emits a table; a runtime executing that table
// parses no templates, reads no descriptors, and resolves no field paths. That
// split is what lets several runtimes in different languages agree on what a
// request means, because none of them decides it.
//
// The IR is deliberately larger than routing needs. Validation rules, resource
// patterns, singular and plural names, and per-binding response sets exist so an
// OpenAPI target can produce a document a human can navigate — see
// README §9 in the gateway repository.
package service
