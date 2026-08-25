# Changelog

All notable changes to protokit are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`service` package: a service-level IR.** Descriptors in, routes out.
  It reads `google.api.http`, `resource`, `field_behavior`, `field_info` and
  `method_signature`, classifies methods against the AIP standard methods
  (131–136, 231–235), resolves path/body/query binding, and derives the status
  set each binding can produce.

  `service/httprule` carries the path-template grammar, the opcode compiler and
  the conflict analysis. **An ambiguous route table now fails the build**,
  naming both bindings and an example path that matches each, rather than being
  resolved by registration order at request time.

- **`header.SetProject`** overrides the generated-file credit line
  independently of the tool name.

  `SetTool` derives the project URL by stripping `protoc-gen-` from the binary
  name. That is right only when a repository is named after its generator:
  `protoc-gen-http` lives in `grpc-gateway-rs`, so the derived link pointed at
  `the-protobuf-project/http`, which does not exist. A generator whose binary
  and repository names differ should call `SetProject` with a value it derives
  from its own module path, so a repository rename cannot leave a dead link.

- **GitHub artifact attestation on releases.** Every release now carries a
  Sigstore bundle over the supply-chain archive, verifiable with
  `gh attestation verify` and nothing else installed.

  It is deliberately redundant with the SLSA provenance: same claim, different
  verifier. `slsa-verifier` is a separate binary a consumer must obtain and
  trust before they can check anything, and the SLSA provenance remains the
  stronger claim — Build L3 from an isolated builder, where the GitHub
  attestation is a self-attestation at L2. Neither replaces the other.

### Changed

- **Release assets are one archive, not four loose files.** A release now
  carries `protokit-<version>-supply-chain.zip` — the SBOM, its cosign
  signature and certificate, and the SLSA provenance — plus the attestation
  over that archive.

  The four files only mean anything together, so shipping them separately
  invited fetching a subset. The attestation stays outside the archive because
  it attests the archive, and it removes the need for a separate checksum:
  `gh attestation verify` computes and checks the digest itself.

  The archive is **not** byte-reproducible and does not claim to be — the SBOM
  embeds its own generation timestamp. Integrity comes from the attested
  digest, which is a different property.

  **Migrating a verification script:** download
  `*-supply-chain.zip` and `*.sigstore.json` instead of the four assets, run
  `gh attestation verify` on the archive, then `unzip` before the existing
  cosign and slsa-verifier steps. See `docs/RELEASE.md`.

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
