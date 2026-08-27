// Package buffers is protokit's message-graph IR: descriptors in, a
// target-agnostic graph of files, messages, fields, enums and services out, with
// every field pinned to a stable target slot.
//
// It is the frontend for generators that emit a *serialization schema* —
// FlatBuffers, Cap'n Proto, ROS 2 IDL, Square Wire — as distinct from one that
// emits storage (the schema IR) or an HTTP surface (the service IR).
//
// # Why the engine needs a third frontend
//
// Reusing one of the other two was the first thing tried, and both fail here for
// reasons that are not fixable by reading them more carefully.
//
// The schema IR folds messages into databases, schemas, tables and columns. That
// fold is exactly right for a generator that stores things and exactly wrong
// here, in three ways. It keeps only the messages that are resources or reachable
// from one, so a plain value object — a Vector3 — has no table and therefore no
// representation, while a .fbs that omits it does not compile. It collapses the
// four 64-bit widths into one neutral type, which a database is right to do and a
// serialization schema is not: Cap'n Proto has a distinct Int64 and UInt64, and
// picking the wrong one silently reinterprets every negative value. And it has
// nowhere to put a oneof, because a table has no union.
//
// The service IR is closer — it carries proto-granular Kinds, oneofs, and the AIP
// resource index, and this package's Kind is deliberately spelled the same way so
// the two read alike. But service.Build only materializes messages reachable from
// a service method, and a .proto file of pure messages with no service at all is
// the single most common input a serialization generator gets. A schema that
// disappears when you delete the service is not a schema.
//
// So the graph is built here, and what the rest of protokit still supplies is
// everything above the IR: naming, the reproducible banner, the template helper,
// the manifest, the golden harness, and the factory's Source/Target/Registry.
//
// # The seam
//
// protokit imports no annotation module — boundary_test.go fails the build if any
// package here does — so the options that configure this graph arrive through an
// [AnnotationReader] the generator registers, in the same shape
// schema.StructureReader uses for the schema IR. A generator supplies its own
// vocabulary; nothing in this package privileges a particular one, and a .proto
// carrying no options at all still builds a complete schema under
// [NoAnnotations]. See annotations.go.
//
// # What makes it a schema rather than a dump
//
// The one property this package exists to protect is that a field's slot in the
// emitted schema never moves. A proto field number is not that slot: proto
// numbers are 1-based and sparse, Cap'n Proto ordinals are 0-based and
// contiguous, and FlatBuffers ids are 0-based, contiguous, and consumed two at a
// time by a union. Every target therefore needs a mapping, and a mapping that is
// recomputed from scratch on each run is a mapping that silently changes when
// someone deletes a field.
//
// ordinal.go derives that mapping, lock.go records it in a ledger the generator
// commits alongside the .proto, and the two disagreeing is a diagnostic rather
// than a coin flip. See ordinal.go for the derivation rules and why a deleted
// field must be `reserved`.
//
// # Method classification
//
// This package classifies AIP-131..136 methods by prefix matching, which is less
// than the service IR does with the same names, and the reason is worth
// recording. service.Build reaches its classification by building the whole route
// table, and a route table with two overlapping google.api.http templates fails
// the build. That is exactly right for a gateway and an unrelated reason for a
// .capnp file to fail to generate. The classification is used here only to shape
// a ROS .srv and to order an interface's methods, so the cheaper answer is the
// proportionate one.
package buffers
