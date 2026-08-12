# Changelog

All notable changes to protokit are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Removed

- **BREAKING: `protokit.v1` is gone.** The proto module (`protobuf/protokit/v1/`)
  and its committed stubs (`protobuf/protokitpbv1/`) are deleted. protokit now
  reads AIP and nothing else; every annotation vocabulary, the neutral one
  included, arrives through a `StructureReader` the plugin registers.

  The vocabulary moved to store as `entity.v1`
  (`buf.build/the-protobuf-project/entity`), with the reader over it shipped as
  the nested module `github.com/the-protobuf-project/store/entity`. **Protos
  annotated with `(protokit.v1.*)` must migrate to `(entity.v1.*)`**; the option
  shapes and field numbers are unchanged, so the migration is the import line, the
  option prefix, and a `buf dep update`.

  Two reasons, in order of weight. The vocabulary is persistence-shaped —
  `datasource`, `table`, `column`, `id_strategy` — and web3 has no datasources and
  no tables, so holding it here made the neutral engine a persistence engine with
  a generic name and handed every future non-relational plugin a vocabulary it
  could not use. And the import-boundary test added in 1.3.0 carried an allowlist
  entry for it: the gate enforcing "protokit imports no annotation module" had an
  exemption for the one module protokit imported.

  What guarantees two plugins still derive the same neutral names is a *shared
  reader implementation* they both import, not protokit owning the proto. See
  [docs/ownership.md](docs/ownership.md), rule 4.

- **The BSR publish workflow** (`.github/workflows/publish.yml`), along with
  `buf.yaml`, `buf.gen.yaml`, and CI's `Proto` and `API lint` jobs. With no proto
  module in this repository there is nothing to publish, which also retires the
  `BUF_TOKEN` requirement for protokit.

### Changed

- **Structure precedence.** With no vocabulary of its own to read first, protokit
  resolves structure from the registered `StructureReader`s in sorted `Key` order
  (first non-empty answer wins), then the `LayoutResolver`, then its own defaults.
  Where two readers set the same option, the first wins and the second is reported
  as a `lint` diagnostic naming both — previously this diagnostic named
  `protokit.v1` as the fixed winner.

- **Deprecation diagnostics no longer name a replacement vocabulary.** protokit
  owns no annotation module and so knows of none; a `DeprecatedStructure` reader
  supplies that half through `StructureDeprecation()`, which is rendered after an
  em dash as before.

- **`TestNoPluginProtoImports` has no exemptions.** `allowedExact` is removed, and
  `protokit.v1`'s former stub path is pinned in `TestBoundaryPatterns` as a
  *violation* — re-adding the module by re-adding the exemption now fails.

The Go SPI is unchanged: every signature pinned by `spi_test.go` in 1.3.0 still
holds. What changed is which annotations protokit reads, which is a behavioral
break for any proto carrying `(protokit.v1.*)` options.

## [1.3.0] — 2026-08-11

The stabilization release. **From this tag forward the SPI is frozen: additive
changes only until v1.4.0.** No behavior changed — this release adds the tests,
gates, and documentation that make the existing contract hold.

### Added

- **Import-boundary gate.** `TestNoPluginProtoImports` walks every non-test Go
  file and fails if protokit imports a proto module outside
  `google.golang.org/protobuf`, `google.golang.org/genproto/googleapis/api`, and
  protokit's own `protokit.v1`. It runs as its own `Import boundary` CI job so a
  violation shows up as a named check rather than a line in a test log.

  The classifier is deliberately wider than "`*/pb/*` or `*/v1`": a plugin's
  annotation module compiles to `web3pbv1` or `ormpbv1`, which matches neither
  pattern and is exactly the import that matters. It is also narrower in one
  place — a bare `/v2` is a Go module major version, not an API version, and is
  not flagged.

- **SPI freeze assertions** (`spi_test.go`). Compile-time pins on every signature
  in the frozen surface, including the deprecated `Backend` shims. A signature
  change now breaks the build in this repository rather than downstream at
  upgrade time.

- **End-to-end golden case for the deprecated `Backend` path**
  (`golden/testdata/backend_shim/`). web3 stays on `schema.Backend` for months and
  nothing else exercised it, so a build change could have broken every `Backend`
  consumer while protokit's own suite stayed green. The case drives
  `golden.RunCase` through a fake Backend covering `ReadDatasource`, `ReadTable`,
  `ReadColumn`, `Enrich`, and `DedupeSchemaTable`, and diffs the resulting IR
  against a committed golden.

- **`TestNeutralTypesStayPlain`.** Asserts that `Datasource`, `TableStructure`,
  `ColumnStructure`, and `IDStrategy` are built from Go builtins and protokit's own
  types, with no proto message anywhere in their field graph — the import boundary
  checked from the other side.

- **[`docs/ownership.md`](docs/ownership.md).** Where schema vocabulary lives and
  why: protokit owns the SPI and the neutral Go types and imports no annotation
  module; an annotation module lives in the repo that owns the concept; consumers
  depend on annotation modules through the BSR rather than by cloning; and shared
  vocabulary is guaranteed by a shared reader implementation, not by protokit
  ownership.

### Fixed

- **Golden trees are pinned to LF** (`.gitattributes`). Golden comparison is
  byte-for-byte, and git's default `core.autocrlf=true` on Windows rewrites every
  committed LF to CRLF on checkout — so a golden tree fails on the line ending
  alone. This surfaced on the new Backend case and would have broken the
  `windows-latest` and `windows-11-arm` CI jobs.

  **Downstream modules driving `golden.RunPluginCase` or `golden.RunCase` with
  their own testdata need the same rule**, or their cases fail on Windows:

  ```gitattributes
  <testdata dir>/** text eol=lf
  ```

  Pin the input `.proto` files as well as the expected output: doc comments are
  read from the proto source and forwarded into generated output, so a CRLF input
  can put a stray carriage return inside a golden line rather than only at the end
  of one. `golden.RunPluginCase`'s doc comment now states this.

### Deprecated

No new deprecations. The following remain deprecated and fully supported, and are
now covered by the golden case and the freeze assertions above:

- `schema.Backend` → implement `schema.FacetReader` (optionally with
  `schema.StructureReader` and `schema.Enricher`) plus a `schema.LayoutResolver`.
- `protokit.BuildIR` → `protokit.Build`, which returns the full `IR`.
- `protokit.Run` → `protokit.RunPlugin`, which takes a `protokit.Plugin`.
- `golden.RunCase` → `golden.RunPluginCase`.

Each will be removed one major after its consumers have migrated. The neutral
config types (`Datasource`, `TableStructure`, `ColumnStructure`, `IDStrategy`) are
**not** deprecated — `StructureReader` still uses them.

## [1.2.0] — 2026-08-11

### Added

- `protokit.v1` table options and the facet model (#2).

### Changed

- Bumped the GitHub Actions group (#3).

## [1.1.0]

- GraphQL dialect abstraction and introspection support (#1).

## [1.0.0]

- Initial release.

[Unreleased]: https://github.com/the-protobuf-project/protokit/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/the-protobuf-project/protokit/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/the-protobuf-project/protokit/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/the-protobuf-project/protokit/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/the-protobuf-project/protokit/releases/tag/v1.0.0
