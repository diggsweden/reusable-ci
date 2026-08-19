# Open questions from the test review

Behaviours found while reviewing the test suite that look deliberate enough not
to change unasked, but odd enough to be worth a decision. Each was confirmed by
running the code, not by reading it.

Most of the original set turned out to be answerable and has been fixed; those
are listed at the bottom with the commit that closed them. What follows is what
is still open.

## Signing zero packages succeeds

`GPGSignPackages` returns `count = 0` and prints "GPG-signed 0 package(s)" when
the directory holds no `.deb`, `.rpm` or `.apk`.

This is harder to call than it first looks, and the answer depends on a gap
elsewhere:

- **Nothing in this repository calls `release gpg sign-packages`.** It appears
  in no workflow and no script — only in the generated CLI reference — while
  its siblings `gpg import` and `gpg cleanup` *are* invoked from
  `publish-maven-central.yml` and `release-prepare-stage.yml`.
- **It is not dead surface.** The release path already collects `.deb`, `.rpm`
  and `.apk` as publishable assets *and publishes their `.sig` sidecars*, and
  this command is the only thing that produces those sidecars. A consumer whose
  build emits distro packages gets them published unsigned unless it invokes
  the CLI directly.
- **There is nothing to gate a step on.** Neither `artifacts.yml` nor the
  config plan has any notion of a project producing distro packages, so a
  publish-stage step could only be gated on gpg signing being enabled — true
  for most releases, nearly all of which produce no packages. Under that
  design, succeeding silently on zero is correct, and an `--expect-packages`
  flag mirroring `--ledger-expected-count` would be wrong.

So the real question is not the zero case. It is whether `artifacts.yml` should
be able to declare that a project produces distro packages, which would both
give the publish stage something to gate on and make the zero case meaningful.

## A ledger image is signed and attested before its inputs are fully checked

`SignLedgerImages` signs the image, then verifies the pinned SBOM digest. When
the pin does not match, the run aborts with `ErrValidation` and produces no
attestation — but the cosign signature has already been published, and for
keyless signing a transparency log entry with it.

Confirmed by probe: with a deliberately wrong `SBOMSHA256`, the recording
signer holds one `SignImage` call and zero `AttestImage` calls. Recorded in
`TestSignLedgerImages_PremadeSBOMPin` so the ordering cannot change silently.

Whether it matters depends on what a verifier requires. A consumer checking
only the signature would accept an image whose SBOM pin was rejected; one that
also requires the CycloneDX attestation would not, because none was produced.

The same shape appears again, one step later. A ledger entry declaring a
provenance key the engine computes (`image`, `base`, `ref`, `source`) is
refused with `ErrValidation` — but the collision is found while building the
provenance predicate, by which point the image is signed *and* its CycloneDX
SBOM is attested. Recorded in `TestSignLedgerImages_ProvenanceExtras`.

Both checks are pure functions of inputs the run already holds: a file hash,
and a set of key names. Neither depends on anything signing or attesting
produces, so both could run before the first irreversible publish, and a
refused run would leave nothing behind. That is a change to the order of
operations inside the signer boundary, which
[ADR 0002](adr/0002-signer-trust-boundary.md) governs, so it is recorded rather
than made.

## Four more path-safety rules to converge

`internal/pathsafe` now holds the workspace-relative path rule, and the three
implementations that had grown in `app/release` and `domain/pipeline` call it.
Four others remain, each wrapping the same underlying rule in a different
shape:

| Where | Shape |
|---|---|
| `domain/artifact/safe.go` `SafeJoin` | validates *and* joins under a root; also rejects backslash separators |
| `app/validate/workspacedir.go` `safeWorkingDir` | validates, cleans, and returns the path |
| `domain/config/validate.go` | returns violation strings for the artifacts.yml report, not an error |
| `app/release/assemble.go` | applies the rule to attach-artifact globs rather than paths |

None is a copy-paste duplicate — each has a different signature, output type and
error class, and two arguably answer a different question (joining under a root;
validating a glob). That is why converging them is a judgement call per site
rather than a mechanical replacement, and why it is recorded here rather than
done in passing.

A guard test was attempted to stop an eighth implementation appearing and was
dropped as not worth its false confidence. Any signature narrow enough to avoid
flagging image-ref parsing and variadic spread is also narrow enough to miss the
substring-matching variant — which is the shape that was actually wrong, since
it misses an OS separator. A guard that cannot catch the bug that motivated it,
while carrying a four-entry allowlist, reads as more protection than it gives.

## `container sign` documents a digest requirement it does not enforce

The command's own documentation says it three times — the doc comment
("`reusable-ci container sign <image>@<digest>`"), the description ("The image
reference must be a digest reference (image@sha256:...)"), and the error text
for a missing argument ("registry/image@sha256:..."). Nothing checks it.
`SignImage` refuses only an empty reference, so `container sign myimage:latest`
signs whatever the tag resolves to at that moment.

The signature cosign produces is still bound to a digest, so this is not a
broken signature — it is a "sign what you verified" question. If the tag moved
between build and sign, the signed image is not the built one, and nothing in
the run would say so.

The codebase already has the check and applies it on the neighbouring path:
`release image verify` refuses a `--ref` that is not digest-pinned, via
`validReleaseImageDigestRef`. Every in-repo caller of `SignImage` passes a
digest-pinned reference, so this is about the CLI surface a consumer drives
directly.

Enforcing the documented contract is small. It is recorded rather than done
because it would start refusing input that is accepted today, and whether any
consumer signs by tag deliberately is not visible from here.

## `gradle.properties` is parsed more strictly than the format allows

`gradleProperty` matches on the literal prefix `version=`, so a file written as

```properties
version = 1.2.3
```

is not recognised. `java.util.Properties` — the format gradle.properties is —
accepts whitespace around the separator, as well as `:` and a bare space as
separators.

The consequence is quiet: `GradleMetadata` treats an unparsed version as absent,
which is deliberately a warning rather than a failure ("some projects compute
version in build.gradle(.kts)"). So a project using the spaced form gets
"version not found in gradle.properties" and an empty version output, having
written a perfectly valid file.

Widening the match is a few characters. It is recorded rather than done because
the same helper reads other keys, and because "absence is not an error" means
the blast radius of getting it wrong is a wrong version rather than a failed
run — which is the worse direction. Covered as a documented row in
`TestGradleMetadata_WarnsWhenMissing` so the behaviour is at least visible.

## `SanitizePathToken` lets `..` through, and says it does not

Its doc comment is explicit: "The result is safe for filesystem paths, Docker/OCI
tags, and artifact basenames." `internal/app/sbom/generate.go` relies on that by
name — "Sanitised here, once: every layer filename is derived from these, so the
subject that flows down is already path-safe."

`.` and `-` are both in the allowed set, so `..` passes through untouched, and
the trailing-dash trim can *produce* it from an input that did not look like
traversal. Probed:

| Input | Token | Resulting SBOM path |
|---|---|---|
| `app` | `app` | `.reusable-ci/go-build-sbom/app/bom.json` |
| `..` | `..` | `.reusable-ci/bom.json` |
| `-..-` | `..` | `.reusable-ci/bom.json` |
| `.` | `.` | `.reusable-ci/go-build-sbom/bom.json` |
| `../../etc` | `..-..-etc` | (contained) |

**The escape is bounded to one level and cannot leave the workspace**, because
`/` and `\` are themselves mapped to `-`, so no token can contain a separator
and only a single `..` is reachable. So this is not a traversal vulnerability.

What it does break is the canonical path. `GoBuildSBOM` promises to write "into
the canonical reusable-ci path", and a project named `..` gets its bom.json one
directory above it, where the workflow that collects
`.reusable-ci/go-build-sbom/*/bom.json` will not find it — a silently missing
SBOM rather than a wrong one.

The fix is one line — treat a token of `.` or `..` as unusable and return `""` —
and every caller in `app/build` already handles `""` as "not a valid path token".
It is recorded rather than made because the other three call sites
(`app/sbom/generate.go` twice, `snapshotversion.go`) have not been checked for
what they do with an empty token, and changing a shared sanitiser to satisfy one
caller is how the path-safety sprawl above started.

## An unreachable branch in `readGoModulePath`

`readGoModulePath` refuses an empty module path:

```go
module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
if module == "" {
    return "", fmt.Errorf("go.mod module path is empty: %w", errs.ErrInvalidConfig)
}
```

It cannot fire. The line was already `strings.TrimSpace`d before the
`HasPrefix(line, "module ")` test, so it cannot end in whitespace; for the
prefix to match there must be a non-space character after it. Every input that
looks like it should reach this branch — `module`, `module   `, `module\t` —
fails the prefix test instead and falls through to "module directive not
found", which returns the same sentinel.

Confirmed by probe over those inputs: prefix NO MATCH in every case.

Harmless, and the two paths agree on `ErrInvalidConfig`, so nothing observable
differs. Left in place because removing it is a product change and the guard
reads as intentional defence; noted so the next reader does not spend the same
time on it, and so no test claims to cover it. `TestGoMetadata_Refusals` names
its case for the branch it actually reaches.

## Still open in the threat model

- [Profile-dependent `externalParameters` reserved keys](threat-model.md) — a
  caller can declare `source` under the forgejo profile, where the engine does
  not compute it and so does not reserve it. Confirmed not reachable through
  any shipped workflow; worth removing as hygiene.
- [Vacuous OCI release identity match](threat-model.md) — an empty expected
  identity matches an image carrying no labels. Confirmed not reachable: both
  callers require a tag and a commit, so the check fails closed.

## Resolved

| Finding | Closed by |
|---|---|
| `assemble-dist --path` ran `os.RemoveAll` over a path validated only for being non-empty and single-line, deleting directories outside the workspace | `824f492c` |
| Metadata labels were published with empty values, so a build with no commit shipped `org.opencontainers.image.revision=` | `42321763` |
| `checksums --output some/dir/file` created the parent directory under `--assembly` and failed without it; the two modes had two copies of the writing half | `502e9ef3` |
| `DownloadArtifacts` did not validate the transfer item path that decides where files are unpacked, while `AssembleDist` validated the same field on the same plan | `63b1e533` |
| A transfer plan refused on a later item had already downloaded the earlier ones | `63b1e533` |
| The container SBOM was generated once per declared artifact type, identically, each run overwriting the last at the cost of a full image scan | `64fd043e` |
| `internal/livetest` test build was broken (`undefined: requestErr`), so `go test ./...` could not pass | fixed outside this review |

One correction worth recording: the container SBOM duplication was **not** an
oversight. `GenerateContainer`'s doc comment described the loop as
"observationally idempotent", which is true of the output and not of the work.
The fix keeps every declared type named in the operator output and scans once.
