# Verifying a protokit release

Every `v*` tag publishes a CycloneDX SBOM, a keyless Sigstore signature over it,
and SLSA Build Level 3 provenance. This page is the independent-verification
procedure: it assumes you are a third party who trusts neither this repository
nor whoever handed you the files, and it uses nothing but public transparency
logs.

Nothing here requires credentials, a GitHub account, or network access to
anything but the release assets and the public Sigstore infrastructure.

---

## What a release contains

| Asset | What it is |
| --- | --- |
| `protokit-<version>.cdx.json` | CycloneDX 1.6 SBOM — protokit's own component metadata plus the full dependency graph |
| `protokit-<version>.cdx.json.sig` | Detached cosign signature over the SBOM bytes |
| `protokit-<version>.cdx.json.pem` | Short-lived Fulcio X.509 certificate binding that signature to the workflow that produced it |
| `protokit-<version>.cdx.json.intoto.jsonl` | SLSA v1.0 Build L3 provenance attesting how and where the SBOM was built |

The certificate is valid for roughly ten minutes and has long since expired by
the time you read this. That is intentional and does not affect verification —
validity is anchored by the Rekor transparency-log entry, which proves the
signature existed while the certificate was live. Both tools check the log for
you.

### What the provenance covers, and what it does not

protokit is a pure Go library with no `package main`, so **the attested subject
is the SBOM, not an executable.** The provenance answers "was this SBOM produced
by protokit's release workflow, at this tag, on an isolated GitHub-hosted
builder?"

It is not a claim about the module bytes you compile. Those arrive over the Go
module proxy, and their integrity is enforced by the Go checksum database
against your `go.sum`. That check is in
[Verifying the module itself](#verifying-the-module-itself), and for a library
it is the one that governs the code you actually run. Neither check substitutes
for the other.

---

## Prerequisites

**cosign v2.x** — the line these releases are signed with:

```bash
# macOS
brew install cosign

# Linux
curl -sSfLo cosign https://github.com/sigstore/cosign/releases/download/v2.6.5/cosign-linux-amd64
chmod +x cosign && sudo mv cosign /usr/local/bin/
```

> **Do not use cosign v3 for these commands.** v3 removed `--certificate` and
> `--signature` from `verify-blob` in favour of a single combined `--bundle`
> file. These releases publish detached `.sig` + `.pem`, so the commands below
> need the v2 line. v2.6.5 is current and maintained in parallel with v3 — it is
> not an outdated release.

**slsa-verifier v2.7.1**:

```bash
go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@v2.7.1
```

Or grab a release binary from
<https://github.com/slsa-framework/slsa-verifier/releases>.

---

## Step 1 — Download the assets

Set the version once; every later command reuses it.

```bash
VERSION=v1.3.0
REPO=the-protobuf-project/protokit
```

With the GitHub CLI:

```bash
gh release download "$VERSION" \
  --repo "$REPO" \
  --pattern '*.cdx.json' \
  --pattern '*.cdx.json.sig' \
  --pattern '*.cdx.json.pem' \
  --pattern '*.cdx.json.intoto.jsonl'
```

Or without any GitHub tooling:

```bash
BASE="https://github.com/${REPO}/releases/download/${VERSION}"
curl -sSfLO "${BASE}/protokit-${VERSION}.cdx.json"
curl -sSfLO "${BASE}/protokit-${VERSION}.cdx.json.sig"
curl -sSfLO "${BASE}/protokit-${VERSION}.cdx.json.pem"
curl -sSfLO "${BASE}/protokit-${VERSION}.cdx.json.intoto.jsonl"
```

---

## Step 2 — Verify the SLSA provenance

This establishes that the SBOM was built by protokit's release workflow on a
SLSA L3 builder. Copy it verbatim.

```bash
slsa-verifier verify-artifact \
  --provenance-path "protokit-${VERSION}.cdx.json.intoto.jsonl" \
  --source-uri "github.com/the-protobuf-project/protokit" \
  --source-tag "${VERSION}" \
  "protokit-${VERSION}.cdx.json"
```

Success ends with:

```
Verified build using builder "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v2.1.0" at commit <sha>
Verifying artifact protokit-v1.3.0.cdx.json: PASSED

PASSED: SLSA verification passed
```

Anything else — in particular `FAILED: SLSA verification failed`, or a non-zero
exit status — means the artifact failed verification. Treat the file as
untrusted.

### The expected `--source-uri`

```
github.com/the-protobuf-project/protokit
```

Note the form: **no `https://` scheme and no `.git` suffix.** slsa-verifier
compares this against the source repository recorded inside the provenance, and
a mismatch is reported as `expected source ... to match source-uri ...`. This is
the flag that stops an attacker from handing you a validly-signed provenance
produced by *their* repository — without it, any SLSA-attested artifact from
anywhere on GitHub would satisfy the check.

`--source-tag` additionally pins the git tag the artifact was built from. Use
`--source-versioned-tag` instead if you want semver-aware matching.

### Pinning the builder

For the strongest assertion, also pin *which builder* was allowed to produce the
provenance, so a future compromise of a different SLSA builder cannot satisfy
your check:

```bash
slsa-verifier verify-artifact \
  --provenance-path "protokit-${VERSION}.cdx.json.intoto.jsonl" \
  --source-uri "github.com/the-protobuf-project/protokit" \
  --source-tag "${VERSION}" \
  --builder-id "https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@refs/tags/v2.1.0" \
  "protokit-${VERSION}.cdx.json"
```

The builder ID ends in `@refs/tags/vX.Y.Z` because the generator is referenced
by tag rather than by commit SHA — an intentional exception to normal
action-pinning practice, explained in `.github/workflows/release-module.yml`.
Bump the version in this flag when the workflow bumps the generator.

### Reading the provenance

```bash
slsa-verifier verify-artifact \
  --provenance-path "protokit-${VERSION}.cdx.json.intoto.jsonl" \
  --source-uri "github.com/the-protobuf-project/protokit" \
  --print-provenance \
  "protokit-${VERSION}.cdx.json" | jq .
```

The `subject[].digest.sha256` field must equal the SBOM's own hash:

```bash
sha256sum "protokit-${VERSION}.cdx.json"
```

---

## Step 3 — Verify the cosign signature

Independent of the provenance, and over the same bytes.

```bash
cosign verify-blob \
  --certificate "protokit-${VERSION}.cdx.json.pem" \
  --signature   "protokit-${VERSION}.cdx.json.sig" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity "https://github.com/the-protobuf-project/protokit/.github/workflows/release-module.yml@refs/tags/${VERSION}" \
  "protokit-${VERSION}.cdx.json"
```

Success prints exactly:

```
Verified OK
```

Any other output means the artifact failed verification. Do not "retry without
the identity flag".

### What each flag is asserting

`--certificate-identity` is the load-bearing one. It pins **which workflow, in
which repository, at which git ref** was allowed to sign. Its value is the `SAN`
embedded in the Fulcio certificate, and it has this exact shape:

```
https://github.com/<owner>/<repo>/.github/workflows/<workflow-file>@<ref>
                   ▲                ▲                                  ▲
                   │                │                                  └─ refs/tags/v1.3.0
                   │                └─ release-module.yml
                   └─ the-protobuf-project/protokit
```

Omitting it, or loosening it to a bare regex like `.*`, reduces the check to
"somebody, somewhere, signed this with Sigstore" — not a security property. Both
`--certificate-identity` and `--certificate-oidc-issuer` are mandatory in cosign
v2 precisely to stop that.

### Optional: assert the GitHub provenance fields

The Fulcio certificate carries GitHub-specific X.509 extensions. Asserting them
narrows the claim further — useful when codifying this in a policy engine.

```bash
cosign verify-blob \
  --certificate "protokit-${VERSION}.cdx.json.pem" \
  --signature   "protokit-${VERSION}.cdx.json.sig" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity "https://github.com/the-protobuf-project/protokit/.github/workflows/release-module.yml@refs/tags/${VERSION}" \
  --certificate-github-workflow-repository "the-protobuf-project/protokit" \
  --certificate-github-workflow-ref "refs/tags/${VERSION}" \
  --certificate-github-workflow-trigger "push" \
  "protokit-${VERSION}.cdx.json"
```

To read the certificate rather than assert against it:

```bash
openssl x509 -in "protokit-${VERSION}.cdx.json.pem" -noout -text \
  | grep -A2 'Subject Alternative Name'
```

### If the release was produced through `workflow_call`

`release-module.yml` is also reusable. When a sibling repository invokes it,
Fulcio records the identity of the **called** workflow, so the `@<ref>` suffix
is the ref of *this* repository's workflow file rather than the caller's tag.
Match on a pattern instead:

```bash
cosign verify-blob \
  --certificate "protokit-${VERSION}.cdx.json.pem" \
  --signature   "protokit-${VERSION}.cdx.json.sig" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity-regexp '^https://github\.com/the-protobuf-project/protokit/\.github/workflows/release-module\.yml@refs/(tags|heads)/.+$' \
  "protokit-${VERSION}.cdx.json"
```

Keep the anchors (`^`/`$`) and the escaped dots. An unanchored pattern will
match hostile identities that merely *contain* the expected string.

---

## Step 4 — Confirm tampering is actually detected

A verification step you have never seen fail is a verification step you should
not trust. Prove both checks have teeth:

```bash
cp "protokit-${VERSION}.cdx.json" /tmp/tampered.cdx.json
printf '\n' >> /tmp/tampered.cdx.json   # one byte is enough

# Must fail with: FAILED: SLSA verification failed
slsa-verifier verify-artifact \
  --provenance-path "protokit-${VERSION}.cdx.json.intoto.jsonl" \
  --source-uri "github.com/the-protobuf-project/protokit" \
  --source-tag "${VERSION}" \
  /tmp/tampered.cdx.json

# Must also fail
cosign verify-blob \
  --certificate "protokit-${VERSION}.cdx.json.pem" \
  --signature   "protokit-${VERSION}.cdx.json.sig" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --certificate-identity "https://github.com/the-protobuf-project/protokit/.github/workflows/release-module.yml@refs/tags/${VERSION}" \
  /tmp/tampered.cdx.json
```

If either prints a success line, stop — something is wrong with your toolchain,
not with the release.

---

## What this does and does not prove

**Proves.** The SBOM bytes you hold are byte-identical to those produced by
`release-module.yml` in this repository, at the stated tag, on an isolated
GitHub-hosted builder the build itself could not influence — and both the
signature and the provenance are recorded in a public append-only transparency
log. Nobody, including a maintainer with full repository admin, can forge that
pairing without it being publicly logged.

**Does not prove.** That the SBOM's *contents* accurately describe the module.
The signature and provenance attest to authorship, build integrity and
tamper-evidence — not truthfulness. An SBOM generated from a compromised
dependency tree would still be validly signed and validly attested. Verify the
module itself as well.

---

## Verifying the module itself

For a library this is the check that actually governs the code you compile,
because the SBOM is metadata and the module is what your build consumes.

Go verifies every module against the public
[checksum database](https://sum.golang.org) automatically. Query it directly:

```bash
curl -s "https://sum.golang.org/lookup/github.com/the-protobuf-project/protokit@${VERSION}"
```

which returns the authoritative hashes, for example:

```
github.com/the-protobuf-project/protokit v1.2.1 h1:iak7iZZzYg+lYVdH7ZAO4kKGwz3ZX/0s+JK6Ykah4/A=
github.com/the-protobuf-project/protokit v1.2.1/go.mod h1:HODRJtLQhT39oud+Hc7hIxK4PQlhaJ6y8qU11dBpPn4=
```

The `h1:` line must match the corresponding entry in your project's `go.sum`. To
re-verify everything already downloaded for your build:

```bash
go mod verify
```

Ensure the checks are actually on — they are by default:

```bash
go env GOSUMDB GOPROXY GOPRIVATE GONOSUMDB GONOPROXY GOFLAGS
```

`GOSUMDB` should read `sum.golang.org` and `GOPROXY` should include
`https://proxy.golang.org`. If `GOPRIVATE` or `GONOSUMDB` matches
`github.com/the-protobuf-project/*`, checksum verification is being skipped for
this module and none of the above is being enforced.

---

## Reading the SBOM

```bash
# Spec version, component identity, and graph size
jq '{
  spec: .specVersion,
  component: .metadata.component.name,
  version: .metadata.component.version,
  components: (.components | length),
  dependencies: (.dependencies | length)
}' "protokit-${VERSION}.cdx.json"

# Every dependency and its resolved version
jq -r '.components[] | "\(.name)\t\(.version)"' "protokit-${VERSION}.cdx.json" | column -t
```

The SBOM is standard CycloneDX 1.6 JSON, so it loads directly into Grype, Trivy,
Dependency-Track, or any other CycloneDX-aware scanner:

```bash
grype "sbom:protokit-${VERSION}.cdx.json"
trivy sbom "protokit-${VERSION}.cdx.json"
```

---

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| `expected source ... to match source-uri` | `--source-uri` is wrong. It takes the bare `github.com/owner/repo` form — no `https://`, no `.git`. |
| `FAILED: SLSA verification failed` | The artifact does not match its provenance, or the tag/builder assertion does not hold. |
| `no matching signatures` | `--certificate-identity` does not match the signing workflow. Check the tag in the URL, and that you used `release-module.yml`. |
| `unknown flag: --certificate` | You are on cosign v3. These releases use detached `.sig`/`.pem`; install the v2 line. |
| `error verifying entry in tlog` | Network egress to `rekor.sigstore.dev` is blocked. |
| `certificate expired` | You passed `--insecure-ignore-tlog`. Remove it — the log entry is what makes the expired certificate verifiable. |
| Provenance asset missing from an older release | Provenance was added in this workflow; releases tagged before it carry only the SBOM, `.sig` and `.pem`. |
