// Package apispec is protokit's source-neutral model of an API contract.
//
// Every non-proto frontend (GraphQL SDL/introspection, OpenAPI 2.0/3.0/3.1)
// normalizes into this one shape, so a generator that turns "an API someone
// already has" into something else — an AIP proto tree, a client, docs — reads
// one model instead of re-walking each source format. It answers *what does
// this API expose*: named types with typed, constrained fields; operations with
// their transport binding, parameters, bodies and results; and the grouping
// hints the source itself carries (OpenAPI tags, GraphQL type families).
//
// It deliberately records facts, not conclusions: it never decides which type
// is a resource, what its pattern is, or which field is a foreign key. Those
// are target conventions (AIP for idl2proto) and belong to the consumer. What
// the source states explicitly — a field is read-only, a string is a uuid, a
// path is /users/{user_id}/posts — is preserved losslessly so the consumer can
// draw those conclusions.
package apispec
