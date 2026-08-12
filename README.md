# Protokit

**A generic toolkit for building code generators.**

Its core does the hard, backend-agnostic 80% of a *schema* generator — parsing the
descriptor set, reading the [Google AIP](https://google.aip.dev/) annotations
(`google.api.resource` / `field_behavior` / `resource_reference`), and building a
normalized intermediate representation (IR) of databases, tables, columns,
relations, enums, and indexes — so a generator only has to supply the 20% that is
actually specific to its target: how to read its own options and how to render.

Beyond that proto core it also provides the pieces a generator needs to grow past
"one proto source, one output": a **source-agnostic [factory](#beyond-proto--sources-targets-and-languages)**
(generic `Source`/`Target`/`Registry` over a plugin-defined model, so a plugin can
add non-proto sources and multiple output languages), a **[GraphQL frontend](#beyond-proto--sources-targets-and-languages)**
(introspection/SDL → IR → typed client), and shared Go-emit helpers — all reused
across every target instead of re-implemented per plugin.

It ships **no binary and no proto module**. It reads AIP and nothing else; every
annotation vocabulary — including the neutral one that decides what things are
*named* — arrives through a reader the plugin registers. Generators import
protokit.

```mermaid
flowchart LR
    P[".proto files"] --> AIP
    subgraph K["protokit — generic engine"]
        AIP["parse + read<br/>google.api.*"] --> IR["build the IR<br/>tables, columns, FKs,<br/>enums, indexes, synthesis"]
    end
    subgraph Author["your generator, e.g. protoc-gen-orm"]
        R["FacetReader<br/>reads your annotations"]
        L["LayoutResolver<br/>reads your config"]
        T["Target<br/>renders output"]
    end
    R -.->|attaches facets| IR
    L -.->|naming policy| IR
    IR --> T
    T --> OUT["generated files"]
```

## Why

Writing a `protoc` plugin that emits a database schema, an on-chain contract, or
any other model artifact means re-implementing the same frontend every time:
walk the descriptors, honor AIP, group messages into a schema tree, resolve
foreign keys, synthesize surrogate keys and audit timestamps, name and validate
indexes, and template it all out with reproducible banners and golden tests.

protokit implements that once, generically, and exposes a few small interfaces.
Your generator stays focused on its target; two generators built on protokit
(a database one and a blockchain one) share the IR and — enforced by a test —
derive the same names from the same protos.

## The neutral vocabulary: `entity.v1`

Two generators over one set of protos must agree on what a table is called. That
agreement comes from a vocabulary neither of them owns alone — `entity.v1` — which
expresses exactly the structure deciding **what things are named and which of them
exist**, and nothing storage-specific. A Solidity generator and a Postgres
generator must agree on a table name; they have no shared opinion about
`VARCHAR(500)`.

```proto
option (entity.v1.datasource) = {database: "bookstore_db" schema: "bookstore"};

message Author {
  option (google.api.resource) = {type: "bookstore.v1/Author" …};
  option (entity.v1.table) = {id: ID_STRATEGY_ULID, timestamps: true};

  string internal_notes = 4 [(entity.v1.column) = {skip: true}];
}
```

**It does not live here.** It lives in store, published as
`buf.build/the-protobuf-project/entity`, with the reader over it shipped as the
nested module `github.com/the-protobuf-project/store/entity` — which imports
protokit and nothing else from store, so any plugin can consume it without pulling
a database generator along with it.

That is the opposite of the obvious arrangement, and it was arrived at the hard
way: protokit did own this vocabulary, as `protokit.v1`, until the persistence
shape of it (`datasource`, `table`, `column`, `id_strategy`) made the neutral
engine a persistence engine with a generic name. web3 has no datasources. What
makes two plugins agree is that they run **the same reader**, not that protokit
adjudicates. See [docs/ownership.md](docs/ownership.md).

## The extension points

A generator supplies **facet readers** (its own annotations), an optional
**layout resolver** (its own config), and **targets** (rendering):

```go
// FacetReader carries your annotation package into the IR — as side-tables keyed
// by node, never as fields on the IR. protokit imports none of it.
type FacetReader interface {
    Key() string                                          // "orm.v1"
    ReadFile(protoreflect.FileDescriptor) (any, error)
    ReadMessage(protoreflect.MessageDescriptor) (any, error)
    ReadField(protoreflect.FieldDescriptor) (any, error)
}

// LayoutResolver is the naming policy you resolve from your own config file.
type LayoutResolver interface {
    ResolveDatasource(pkg string) (database, schema string, stripVersion, ok bool)
    DedupeSchemaTable() bool
}

// Target renders the finished IR.
type Target interface {
    Name() string                                       // "gorm", "solidity", …
    Generate(p *protogen.Plugin, dbs []*Database) error
}

// IRTarget is a Target that also wants the facets.
type IRTarget interface {
    Target
    GenerateIR(p *protogen.Plugin, ir *IR) error
}
```

Wire them into a `main`:

```go
protokit.RunPlugin(p, opts, protokit.Plugin{
    Registry: map[string]schema.Target{"sql": &sqlTarget{}},
    Readers:  []protokit.FacetReader{myReader{}},
    Layout:   myLayout,
})
```

protokit reads AIP itself, collects each reader's facets, runs any enrichment,
finalizes indexes, then hands the IR to the selected target.
Registration is explicit at `RunPlugin` — there is no global registry and no
`init()`, so a run sees exactly what its caller passed.

Read a facet back by node:

```go
opts, ok := protokit.Facet[*ColumnFacet](ir, "orm.v1", col.Node)
```

`col.Node` is a `NodeID` — the fully-qualified proto name, always derived from the
descriptor. Never from `Table.Name` or `Column.Name`: those are outputs, and a
table rename must not orphan a facet lookup.

Two optional interfaces exist for a reader that must influence the build rather
than merely annotate it — `StructureReader` (the neutral vocabulary itself, a
deprecated one being migrated off, or the referential actions no vocabulary
expresses) and `Enricher` (constraints the index pass reads). Each doc comment
explains why it is unavoidable.

> **Migrating from `schema.Backend`?** It still works — protokit adapts it
> internally — and `Run`/`BuildIR` keep their signatures. Both are deprecated;
> see [`schema/backend.go`](schema/backend.go) for the mapping.

## Two plugins, one set of names

The property all of the above exists to protect:

```go
golden.IRAgreement(t, caseDir, pluginA, pluginB)
```

Builds the IR under both plugins' readers and asserts identical database, schema,
table, and column names plus primary- and foreign-key resolution, naming the
diverging `NodeID` on failure. Before a shared neutral vocabulary, each generator
resolved those names from its own annotations and its own config, so a second
generator over the same protos silently disagreed. This is the test that keeps
that from coming back — and, since protokit no longer reads the vocabulary itself,
it is also what catches a plugin that wrote its own `entity.v1` reader instead of
importing the shipped one.

Its companion, `golden.Determinism(t, caseDir, plugin)`, generates twice and
byte-compares — catching the map-ranged-into-output bug that a committed golden
file cannot.

## The IR

The IR is a plain tree with **neutral, target-agnostic types** — no SQL, no
Solidity. Each generator projects `Column.Type` (a `schema.FieldType`) onto its
own type system.

```mermaid
erDiagram
    Database ||--o{ Schema : has
    Schema   ||--o{ Table  : has
    Schema   ||--o{ Enum   : has
    Table    ||--o{ Column : has
    Table    ||--o{ ForeignKey : has
    Table    ||--o{ Index  : has
    Column   }o--o| Enum   : "may reference"
```

## Beyond proto — sources, targets, and languages

The proto path above (`Backend` + `schema.Target`, driven by `Run`) is the original
core. On top of it, protokit provides a **source-agnostic factory** so a generator can
have more than one input and more than one output language, sharing the orchestration:

```go
// factory — generic over the plugin's model type M.
type Source[M any] interface { Name() string; Build(Ctx) (M, error) }
type Target[M any] interface { Name() string; Languages() []string; Generate(Ctx, M, string) error }
type Registry[M any] struct { Sources map[string]Source[M]; Targets map[string]Target[M] }
```

A generator keeps its own richly-typed model `M` (e.g. one carrying a proto-schema
facet *and* a GraphQL facet); protokit stays free of that model. A **proto source**
wraps `BuildIR`; the DB `schema.Target`s are adapted into `factory.Target[M]`; and the
**GraphQL frontend** (`graphql/introspect` + `graphql/ir` + `graphql/dialect`) is a
second source that turns an endpoint or a `.graphql` SDL file into the IR a client
target renders:

```mermaid
flowchart LR
    P[".proto + AIP"] --> PS["proto source<br/>(BuildIR)"]
    Q["GraphQL endpoint<br/>/ .graphql SDL"] --> QS["graphql source<br/>(introspect / ir / dialect)"]
    PS --> M[("plugin model M<br/>(typed facets)")]
    QS --> M
    M --> T{{"target × language"}}
    T --> DB["database targets"]
    T --> GQL["graphql client"]
```

The parsing is language-agnostic; a target's language-specific templates sit under a
per-language folder, so adding Python/TypeScript output is a new template set over the
same model. [orm](https://github.com/the-protobuf-project/orm) is the reference
consumer of all of this (proto → GORM/SQL/Prisma, plus GraphQL → a typed Go client).

## Packages

| Package | Role |
| --- | --- |
| `protokit` (root) | The frontend: `BuildIR`, `Run`, descriptor traversal, grouping, synthesis, relation + index resolution, diagnostics, `protokit.yaml` layout config. |
| `schema` | The IR types + the `Backend` and `Target` service-provider interfaces + the neutral `FieldType`. |
| `types` | Generic type utilities: `ClassifyField` (proto → neutral type), `Relationalizable`, `ParseProvider`. |
| `naming` | snake/Camel/Pascal, pluralization, identifier sanitizing, **plus Go-emit helpers**: `Doc` (render a description as doc comments), `GoFileName` (safe lowercase file names, build-constraint-guarded), `Unique` (numeric-suffix dedup), `GoKeyword`. |
| `factory` | **Source-agnostic co-generation.** Generic `Source[M]` / `Target[M]` / `Registry[M]` over a plugin-defined model `M`, plus a `Ctx` and a language axis — so one binary drives many sources and targets from one config. |
| `graphql` | **A GraphQL frontend** (parallel to the proto core). `introspect` fetches/decodes introspection JSON and parses `.graphql` SDL; `ir` normalizes it into a GraphQL IR; `dialect` abstracts engine conventions (Hasura built-in). Depends on [gqlparser](https://github.com/vektah/gqlparser). |
| `header` | The reproducible "Code generated by …" banner. |
| `docs` | Mermaid ER diagrams + per-model README rendering (the type column comes from a generator-supplied projector). |
| `templates` | A thin `text/template` render helper. |
| `golden` | An in-process golden-file test harness (compiles `.proto` with [protocompile](https://github.com/bufbuild/protocompile) — no `protoc`/`buf` on PATH). |

## Who builds on it

- **[orm](https://github.com/the-protobuf-project/orm)** — database schemas (GORM, SQL, Prisma) from `orm.v1` annotations.
- **[web3](https://github.com/the-protobuf-project/web3)** — on-chain artifacts (Solidity contracts, a Graph subgraph) from `web3.v1` annotations.

Each is a self-contained example of a protokit generator; see their READMEs for
the full pipeline.

## Releasing / consuming

protokit is a Go library — a release is a semver git tag:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

Consumers then `go get github.com/the-protobuf-project/protokit@v0.1.0`.

## License

Apache-2.0.
