# Ownership

Where schema vocabulary lives, and why it lives there.

This document exists because the layout below is not the obvious one. The obvious
move — when two generators need to agree on what a table is called — is to put the
shared annotation module in protokit, since protokit is what they both already
depend on. That arrangement has been proposed, adopted, and then removed. The
rules here are the outcome, written down so the next person to reach for the
obvious answer finds the reasoning instead of re-deriving a layout we have already
tried.

## The rules

**1. protokit owns the SPI and the neutral Go types. It depends on no plugin and
imports no annotation module — including one of its own.**

protokit reads exactly one vocabulary directly: the standard `google.api.*` (AIP)
annotations. Everything past that arrives through the SPI. A generator implements
[`schema.FacetReader`](../schema/facet.go) over its own annotation module and
passes it to `protokit.Build`; protokit stores the result in a side-table keyed by
`NodeID` and never interprets it.

"Including one of its own" is the part that had to be learned. protokit used to
own `protokit.v1` — a neutral vocabulary of datasource, table, column, and id
strategy — on the reasoning in rule 4 below, and this repository's own import
boundary test carried an allowlist entry for it. A test enforcing "protokit imports
no annotation module" with an exemption for the one module it imported is not
enforcing much.

The deeper problem was what the vocabulary *said*. `datasource`, `table`,
`column`, and `id_strategy` are persistence-shaped. web3 has no datasources and no
tables. Holding that vocabulary made the neutral engine a persistence engine with
a generic name, and every future non-relational plugin — a streams generator, a
cache generator, a docs generator — would have inherited a vocabulary it could not
use. It now lives in store as `entity.v1`.

The neutral Go types in [`schema/backend.go`](../schema/backend.go) — `Datasource`,
`TableStructure`, `ColumnStructure`, `IDStrategy` — are plain Go structs, and stay
exactly as they are. That is not a contradiction: protokit knows what an id
strategy *is* without knowing which annotation expressed it. They are what a
`StructureReader` maps a vocabulary *onto*, and they are the reason protokit needs
no import of that vocabulary.

The buffers IR has its own family of these — the `*Annotations` structs and
`Vocabulary` in [`buffers/annotations.go`](../buffers/annotations.go), which a
`buffers.AnnotationReader` maps a serialization vocabulary onto. Same rule, same
reason, and pinned by the same test: protokit knows what a packed layout or a
pinned ordinal *is* without knowing which annotation expressed it.

This rule is enforced, not just documented: `TestNoPluginProtoImports` in
[`boundary_test.go`](../boundary_test.go) fails the build if any non-test file
imports a proto module outside `google.golang.org/protobuf` and
`google.golang.org/genproto/googleapis/api`. It has no exemptions.

**2. An annotation module lives in the repo that owns the concept.**

`entity.v1` and `store.v1` live in store. `web3.v1` lives in web3. An annotation
module describing entities lives in the repository that owns what an entity means.

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

This is the rule that closes the loop, and the one most often gotten wrong —
including by this project, which got it wrong in the direction of rule 1 for a
release.

When two plugins must agree on a vocabulary, the thing they share is a
`FacetReader` implementation over that vocabulary — shipped from the repository
that owns it, per rule 2, and consumed over the BSR, per rule 3. They do not share
it by moving the module into protokit.

Concretely: `entity.v1` lives in store, and store ships
`github.com/the-protobuf-project/store/entity` — a nested module holding the
generated stubs and the reader over them. It imports protokit and nothing else
from store, so cache, streams, or any other plugin can consume the reader without
pulling gorm, prisma, or graphql along with it. Agreement on neutral names holds
because those plugins run *the same reader*, not because protokit adjudicates.

Moving the module into protokit appears to work, and it does work, right up until
the vocabulary needs to describe something the engine has no business knowing
about. Then protokit's dependency graph is a function of what its consumers happen
to need, which is the inversion rule 1 exists to prevent: every new concept any
plugin wanted to describe becomes a protokit release, and protokit accumulates a
vocabulary for every domain any generator ever touched.

[`golden.IRAgreement`](../golden/agreement.go) is the test that keeps the sharing
honest. Note what it can and cannot prove: it catches two plugins deriving
different neutral names, which is exactly what happens when one of them writes its
own reader instead of importing the shared one. It cannot prove they imported the
same package, so the reader's own package documentation says so in as many words.

## What goes where

| Thing | Lives in | Reaches protokit via |
| --- | --- | --- |
| The SPI (`FacetReader`, `LayoutResolver`, `IR`, `Facet`) | protokit | — |
| Neutral Go types (`Datasource`, `TableStructure`, `ColumnStructure`, `IDStrategy`) | protokit | — |
| Neutral Go types (`buffers.*Annotations`, `buffers.Vocabulary`) | protokit | — |
| `google.api.*` (AIP) | google | read directly |
| `entity.v1` (neutral naming/structure vocabulary) | store, as a nested module | the `entity.Reader()` every plugin imports |
| A generator's own vocabulary (`store.v1`, `web3.v1`, …) | the repo owning the concept | a `FacetReader` the generator passes to `Build` |
| A serialization vocabulary (`buffers.v1`, …) | the repo owning the concept | a `buffers.AnnotationReader` the generator passes to `buffers.Build` |
| Naming policy from a config file | the plugin | a `LayoutResolver` the plugin passes to `Build` |

## If you are about to add a proto import to protokit

Don't. There is no case that needs one. The three that look like they might:

- **A value a target reads back at render time** — a SQL column type, an access
  model, a query surface. That is a facet. Return it from `ReadFile` / `ReadMessage`
  / `ReadField`, and read it with `protokit.Facet[T]` in the target. protokit
  stores it without knowing what it is.

- **A value protokit must act on while building** — a referential action, a
  deprecated option that maps onto a newer equivalent. Implement
  `schema.StructureReader` and map it onto the neutral structs. Those structs exist
  for exactly this, which is why they stay plain Go.

- **A genuinely neutral addition** — something every generator would need to agree
  on. This is the one that used to have an answer here ("then it belongs in
  `protokit.v1`"), and it no longer does. Add it to `entity.v1` in store and extend
  the shared reader, or, if it is neutral in a way `entity.v1` is not — meaningful
  to a generator with no notion of persistence — add a field to the neutral Go
  structs and let each vocabulary map onto it. Neither requires an import here.
