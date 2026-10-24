# Changelog

All notable changes to protokit are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`buffers` package: a message-graph IR for serialization schema.** Descriptors
  in, a target-agnostic graph of files, messages, fields, oneofs, enums and
  services out, with every field pinned to a target slot that does not move
  between runs.

  It is a third frontend rather than a use of the existing two, because both fail
  for a serialization target in ways that are not fixable by configuration. The
  schema IR keeps only resources and what is reachable from them — so a plain
  `Vector3` has no representation, while a `.fbs` that omits it does not compile —
  and it folds the four 64-bit widths into one type, which silently reinterprets
  every negative value in a format that distinguishes `Int64` from `UInt64`. The
  service IR materializes nothing that no method mentions, and a `.proto` of pure
  messages with no service at all is the common input here.

  Slot derivation (`ordinal.go`) and the ledger that records it (`lock.go`) are
  the point of the package: proto field numbers are 1-based and sparse, Cap'n
  Proto ordinals 0-based and contiguous, FlatBuffers ids consumed two at a time by
  a union, and a mapping recomputed per run changes silently when a field is
  deleted. A build that would assign a different slot than the ledger records
  reports it instead of shipping it.

- **`buffers.AnnotationReader`: the vocabulary seam for the new IR.** A generator's
  own options reach the walk as plain neutral structs — descriptor in, struct out —
  in the same shape `schema.StructureReader` uses, so protokit still imports no
  annotation module. `buffers.Vocabulary` carries that vocabulary's option
  spellings for diagnostic hints, so a message telling someone what to type names
  their option rather than one protokit invented, and `buffers.NoAnnotations`
  builds a complete schema from a `.proto` that declares no options at all.

  The nine neutral structs are pinned by `TestNeutralTypesStayPlain` alongside the
  schema IR's, so a field typed as a plugin's option message fails CI rather than
  compiling.

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

- **Foreign keys across databases: `Table.ExternalForeignKeys`.** A reference
  whose target is generated into another database of the same run (a generator
  routing packages into several databases that share one physical database:
  same provider, same URL) becomes a constraint-only foreign key there, with
  `ForeignKey.ReferencedDatabase` naming the target's database. It carries no
  relation — no association, no `HasManyRef`, the column's `FKModel` stays
  empty — because each database renders on its own and cannot name another's
  models; a target that writes DDL into the shared database renders it, every
  other target ignores it. The target is matched by AIP resource type
  (`ForeignKey.ReferencedType`) or, for a parent from a nested pattern, by
  resource pattern (`ForeignKey.ReferencedPattern`), never by model name.

  ON DELETE is NO ACTION unless the reference sets one explicitly, and a
  parent's synthesized CASCADE gives way to it. Rows are soft-deleted (AIP-164),
  and a soft delete is an UPDATE that fires no referential action, so a
  soft-deleted target stays referenceable; the constraint acts only at the
  purge, where it refuses to remove a row that is still referenced instead of
  letting one database's purge delete (or orphan) the other's rows.

  A target in another physical database, or matching more than one table, or
  with no column the values can match, stays a soft FK, and the `ref`
  diagnostic says which. No dangling constraint is emitted.

- **Reference discovery: `Options.References`.** buf invokes a plugin once per
  input, so a module generated in another run — freebusy's protos in one, the
  RFC module's in the next — was never seen, and every reference into it was a
  soft FK. A reference no table of the run matches is now looked up in files
  the run does not generate: first the request's own non-generated files (its
  import closure), then the reference images listed in `Options.References` —
  binary `FileDescriptorSet`s such as `buf build <module> -o x.binpb` writes,
  read only for lookup and never generated. Their tables are built with the
  run's own readers, layout and naming passes, so a discovered constraint names
  the table and column the module gets when it is generated itself. A target
  found this way in the same physical database becomes a constraint-only FK in
  `Table.ExternalForeignKeys`, NO ACTION on delete; one not found anywhere stays
  a soft FK. Discovery runs only on the first miss.

- **`ForeignKey.Kind` (`schema.ReferenceKind`) records how each reference
  resolved:** `ReferenceLocal` (this database, this run), `ReferenceCrossDatabase`
  (another database of this run), `ReferenceDiscovered` (found by discovery,
  not generated here) or `ReferenceUnresolved` (a soft FK), with
  `ForeignKey.Unresolved` (`schema.UnresolvedReason`) saying why: not found,
  ambiguous, another physical database, or no name column.
  `Table.UnresolvedForeignKeys` keeps the soft FKs' records; nothing renders
  from it.

### Fixed

- **A resource_reference resolves by resource type first.** Resolution by
  model name missed every target whose name was qualified on a collision
  (`Resource` → `ResourceResource`), leaving a soft FK to a table in the same
  database. It now matches the target's `google.api.resource` type, then falls
  back to the model name. A reference only the type (or a parent only the
  pattern) resolves gets a constraint-only FK, not a relation: its column has
  always stored the full name as written, and a relation would make the
  generated repositories store the bare id instead. No stored value changes.

- **A reference to a nested resource targets its name column.** Such a column
  stores the whole resource name (a leaf id cannot rebuild it), yet the FK
  pointed at the target's surrogate id, so the constraint would reject every
  row. `ReferencedColumn` is now the target's unique IDENTIFIER (`name`)
  column, and the column's type follows it. Every FK now references the column
  holding what its column stores: the primary key for a relation to a top-level
  resource, a parent FK and an embed FK (bare ids); the name column for a
  relation to a nested resource and for every constraint-only resource_reference
  (full names, AIP-122). A relation to a nested resource with no unique name
  column becomes a soft FK instead of a constraint on the wrong column.

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
