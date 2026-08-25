// Package httprule parses and compiles the path templates of google.api.http,
// the annotation that drives HTTP/JSON transcoding (AIP-127).
//
// The grammar it accepts is the one in google/api/http.proto:
//
//	Template  = "/" Segments [ Verb ] ;
//	Segments  = Segment { "/" Segment } ;
//	Segment   = "*" | "**" | Literal | Variable ;
//	Variable  = "{" FieldPath [ "=" Segments ] "}" ;
//	FieldPath = Ident { "." Ident } ;
//	Verb      = ":" Literal ;
//
// A template is parsed once ([Parse]) and compiled once ([Compile]) at build
// time. Compilation flattens the nested Variable form into a linear sequence of
// match segments plus a set of capture spans, which is the form a runtime can
// execute without carrying a parser: match positionally, then slice out each
// capture. Every gateway runtime generated from this IR ships that executor and
// nothing more, so no two runtimes can disagree about what a template means.
//
// The package also answers the question a router must answer before it serves
// anything: can two of these templates match the same request? [Conflicts]
// decides it exhaustively over a route set, so an ambiguous API is a build
// failure rather than a registration-order coin flip at runtime.
package httprule
