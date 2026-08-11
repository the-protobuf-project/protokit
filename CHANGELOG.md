# Changelog

All notable changes to protokit are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
