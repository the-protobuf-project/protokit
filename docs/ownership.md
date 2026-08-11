# Ownership

Where schema vocabulary lives, and why it lives there.

This document exists because the layout below is not the obvious one. The obvious
move — when two generators need to agree on what a table is called — is to put the
shared annotation module in protokit, since protokit is what they both already
depend on. That arrangement has been proposed, and rejected, more than once. The
rules here are the outcome, written down so the next person to reach for the
obvious answer finds the reasoning instead of re-deriving a different layout.

## The rules

**1. protokit owns the SPI and the neutral Go types. It depends on no plugin and
imports no annotation module.**

protokit reads two vocabularies directly: the standard `google.api.*` (AIP)
annotations, and `protokit.v1` — its own neutral structure, which lives in this
repository because protokit reads it. A vocabulary the engine depends on cannot
live in a plugin that depends on the engine.

Everything past those two arrives through the SPI. A generator implements
[`schema.FacetReader`](../schema/facet.go) over its own annotation module and
passes it to `protokit.Build`; protokit stores the result in a side-table keyed by
`NodeID` and never interprets it.

The neutral Go types in [`schema/backend.go`](../schema/backend.go) — `Datasource`,
`TableStructure`, `ColumnStructure`, `IDStrategy` — are plain Go structs, and stay
that way. They are what a `StructureReader` maps its vocabulary *onto*, and they
are the reason protokit needs no import of that vocabulary. Nothing about
`entity.v1`, `orm.v1`, or `web3.v1` belongs in them.

This rule is enforced, not just documented: `TestNoPluginProtoImports` in
[`boundary_test.go`](../boundary_test.go) fails the build if any non-test file
imports a proto module outside `google.golang.org/protobuf`,
`google.golang.org/genproto/googleapis/api`, and protokit's own `protokit.v1`.

**2. An annotation module lives in the repo that owns the concept.**

`orm.v1` lives in orm. `web3.v1` lives in web3. An annotation module describing
entities lives in the repository that owns what an entity means.

The test is who changes it. A vocabulary evolves with the concept it describes, so
putting it anywhere else turns every change into a cross-repository release: a
version bump in protokit, a tag, a dependency update downstream, for a field that
only one team was ever going to set.

**3. Consumers depend on annotation modules through the BSR, never by cloning.**

A consumer adds the module as a BSR dependency and generates against it. It does
not vendor a copy of the `.proto` files, and it does not clone the repository to
build them.

A cloned proto is a fork with no version on it. The two copies drift, both keep
compiling, and the divergence surfaces as two generators disagreeing about a field
number — which is the failure mode with the worst ratio of debugging time to
cause.

**4. Shared vocabulary is guaranteed by a shared reader implementation, not by
protokit ownership.**

This is the rule that closes the loop, and the one most often gotten wrong.

When two plugins must agree on a vocabulary, the thing they share is a
`FacetReader` implementation over that vocabulary — shipped from the repository
that owns it, per rule 2, and consumed over the BSR, per rule 3. They do not share
it by moving the module into protokit.

Moving it into protokit would appear to work. It would also make protokit's
dependency graph a function of what its consumers happen to need, which is the
inversion rule 1 exists to prevent: every new concept any plugin wanted to
describe would become a protokit release, and protokit would accumulate a
vocabulary for every domain any generator ever touched.

Agreement on *neutral* names — database, schema, table, column — is a separate
guarantee and does not depend on any of this. protokit reads `protokit.v1` itself,
so two generators over one set of protos derive the same names by construction.
[`golden.IRAgreement`](../golden/agreement.go) is the test that keeps that true.

## What goes where

| Thing | Lives in | Reaches protokit via |
| --- | --- | --- |
| The SPI (`FacetReader`, `LayoutResolver`, `IR`, `Facet`) | protokit | — |
| Neutral Go types (`Datasource`, `TableStructure`, `ColumnStructure`, `IDStrategy`) | protokit | — |
| `protokit.v1` (neutral naming/structure vocabulary) | protokit | read directly |
| `google.api.*` (AIP) | google | read directly |
| A generator's vocabulary (`orm.v1`, `web3.v1`, …) | the repo owning the concept | a `FacetReader` the generator passes to `Build` |
| Naming policy from a config file | the plugin | a `LayoutResolver` the plugin passes to `Build` |

## If you are about to add a proto import to protokit

Don't. The two cases that look like they need one:

- **A value a target reads back at render time** — a SQL column type, an access
  model, a query surface. That is a facet. Return it from `ReadFile` / `ReadMessage`
  / `ReadField`, and read it with `protokit.Facet[T]` in the target. protokit
  stores it without knowing what it is.

- **A value protokit must act on while building** — a referential action, a
  deprecated option that maps onto a `protokit.v1` equivalent. Implement
  `schema.StructureReader` and map it onto the neutral structs. Those structs exist
  for exactly this, which is why they stay plain Go.

If neither fits, the vocabulary probably belongs in `protokit.v1` — a genuine
addition to the neutral layer, made here, on purpose, with the API-lint and
breaking-change gates that apply to it. That is a different decision from
importing someone else's module, and it should be made deliberately rather than
arrived at by adding an import.
