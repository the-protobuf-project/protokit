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
