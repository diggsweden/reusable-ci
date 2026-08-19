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

## Re-authenticating leaves a stale `identitytoken` in the auth config

`MergeAuth` replaces the `auth` value for a registry and preserves the entry's
other fields, which is what its doc comment promises: "a prior config, whose
other registries and fields are preserved untouched". Probed:

```json
{"auths":{"ghcr.io":{
  "auth": "<the new credential>",
  "email": "a@b.c",
  "identitytoken": "stale-token"     ← survives
}}}
```

`identitytoken` is not an ordinary field. Docker, podman and containerd prefer
it over `auth` when both are present — it is what `docker login` writes for
registries using token authentication. So a config that already carried one
keeps authenticating with the old token after a fresh login with new
credentials.

Reach is narrow. Nothing in this repository writes `identitytoken`; it can only
arrive from a pre-existing config on the runner — a `docker login` earlier in
the job, a mounted config, or a self-hosted runner whose home directory
persists between jobs. On an ephemeral runner the config starts empty and this
cannot happen.

The fix is one line — delete `identitytoken` from the entry being
re-authenticated, since it is an alternative credential for exactly the
registry whose credential is being replaced. It is recorded rather than made
because "preserve other fields" is deliberate and documented, and narrowing it
to exclude one key is a decision about which fields are credentials rather than
metadata.

Pinned in `TestMergeAuth_ReauthenticatingReplacesTheCredential`, which points
here if the behaviour changes.

## A minimal-header JWT escapes the output redactor

`safeexec.RedactKeyMaterial` scrubs subprocess output before it is folded into
an error and printed to the CI log. Its JWT pattern is:

```go
regexp.MustCompile(`eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`)
```

The `{20,}` floor exists, per the comment, to avoid matching "short coincidental
dot-separated strings like file paths or version numbers". It also excludes the
smallest valid JWT header:

| Header JSON | base64url | chars after `eyJ` | redacted |
|---|---|---|---|
| `{"alg":"HS256"}` | `eyJhbGciOiJIUzI1NiJ9` | 17 | **no** |
| `{"alg":"RS256"}` | `eyJhbGciOiJSUzI1NiJ9` | 17 | **no** |
| `{"alg":"HS256","typ":"JWT"}` | … | 33 | yes |
| `{"alg":"RS256","typ":"JWT","kid":…}` | … | 49 | yes |

`typ` is optional under RFC 7519, so a bare `{"alg":…}` header is valid and
common from minimal issuers. Such a token passes through unredacted.

GitHub's OIDC tokens carry `typ` and `kid`, so the tokens most likely to appear
in this pipeline *are* caught — which is why this is a narrow gap rather than an
open leak.

The fix is to lower the floor on the first segment alone. The `eyJ` prefix is
already highly specific — it is the base64url of `{"` — so a shorter floor there
does not reintroduce the false positives the comment is guarding against; the
existing benign cases (`foo.bar.baz`, `version 1.2.3`, `key.pgp`) do not begin
with `eyJ` at all.

Recorded rather than changed because loosening a redaction pattern deserves its
own review, and because the same floor may be deliberate for the payload and
signature segments. Pinned in
`TestRedactKeyMaterial_MinimalHeaderJWTIsNotRedacted`, which fails once the gap
closes.

## skopeo keeps the signing secrets that syft is given up

`container image-evidence` builds two subprocess adapters in the same function:

```go
skopeoAdapter := skopeo.New()                    // no UnsetEnv
...
&syft.Adapter{UnsetEnv: signerSecretEnv()},      // 12 secrets scrubbed
```

`signerSecretEnv()` lists `COSIGN_KEY`, `COSIGN_PASSWORD`, `GPG_SIGNING_KEY`,
`GPG_SIGNING_PASSWORD`, `REGISTRY_PASSWORD`, `REGISTRY_TOKEN`,
`FORGEJO_TOKEN` and others. syft is spawned without them; skopeo is spawned
with them.

This is not an oversight of design — the machinery is there. `skopeo.Adapter`
has its own `UnsetEnv` field, and `envWithout`, the helper that applies it, is
**byte-identical** between `internal/adapters/skopeo` and
`internal/adapters/syft`. Both were written; only one is wired to a caller.
Nothing anywhere sets `UnsetEnv` on the skopeo adapter.

It is defence in depth rather than a live exposure: skopeo is a trusted binary
and is not known to log or forward its environment. But the whole point of
scrubbing before the syft call is that a subprocess should not hold a signing
key it has no use for, and skopeo has no more use for one than syft does.

Two things to decide:

- **Wire it** — `skopeo.New()` and `skopeo.WithAuthFile()` would need to carry
  the list, or the call site set the field. One line, and it makes the two
  adapters consistent.
- **Deduplicate `envWithout`** — two identical copies of a security helper is
  the shape where one gets fixed and the other does not. A leaf utility under
  ADR 0004's third rule, as `pathsafe` and `listval` already are.

The skopeo copy now has the same scrub test the syft copy has, so the field is
at least proven to work when set.

## Appending a key to `allowed_gpg_keys.asc` does not authorise it

`docs/verification.md` describes the file as "an armored public-key bundle (one
or more `PGP PUBLIC KEY BLOCK` sections concatenated)" and tells operators to
add one with:

```sh
gpg --armor --export <email> >> .reusable-ci/allowed_gpg_keys.asc
```

That `>>` appends a **second armor block**, and only the first is read.
`PrimaryFingerprints` calls `openpgp.ReadArmoredKeyRing`, which decodes one
armor block and returns the entities inside it. Probed:

| File shape | Produced by | Keys parsed |
|---|---|---|
| one block, two keys | `gpg --armor --export A B` | 2 |
| two blocks concatenated | `gpg --armor --export A >> file` (documented) | **1** |

So a signer added exactly as the documentation instructs is not authorised, and
a tag they sign is refused with:

```
tag signer fingerprint … is not authorised (1 key(s) in …/allowed_gpg_keys.asc)
```

The key count is the only hint that the file was read in part; there is no
error and no warning about the trailing blocks.

**This fails closed.** It denies a legitimate signer rather than admitting an
unauthorised one, so it is a correctness and operability problem, not an
authorisation bypass. It bites during key rotation, which is the one time the
file is meant to hold two keys — and the moment when being unable to ship a
release is most costly.

Either half is a small fix, and they point in opposite directions:

- **Read every armor block** — decode in a loop until EOF — so the documented
  workflow works. This widens the allowlist, so it deserves deliberate review.
- **Refuse a file with trailing blocks** so the operator is told, rather than
  silently authorising fewer keys than the file appears to contain.

Recorded rather than fixed because widening an allowlist is a security-policy
change even when the current behaviour is the accident. Both shapes are pinned
in `tags_allowlist_test.go`.

## A failed signature check exits differently for GPG and for cosign

`VerifyArtifactSignature` documents its contract as: "Returns nil on success;
errs.ErrPermissionDenied wrapping the underlying verify error on signature
mismatch."

That holds for one of the two methods.

| Method | Failure carries | Path |
|---|---|---|
| GPG | `ErrPermissionDenied` | `internal/adapters/openpgp` wraps it |
| sigstore (keyless) | raw error | dispatcher returns `VerifyBlob` unchanged |
| KMS | raw error | same |

`verifyCosign` is `return verifier.VerifyBlob(ctx, in, errOut)`, and the real
adapter wraps only with `cosign <args>: %w`. So no layer adds the sentinel on
the cosign side.

The consequence is an exit code that depends on how the artifact was signed
rather than on what went wrong. A workflow distinguishing "this artifact is not
trustworthy" (EX_NOPERM, 77) from "the tool failed" gets the right answer for a
GPG-signed artifact and the wrong one for a sigstore- or KMS-signed artifact —
and sigstore is the default for container and blob signing here.

The fix is one wrap in `verifyCosign`. It is recorded rather than made because
it changes an exit code, and because the same question applies to the other
cosign verify paths (`container verify`, `release image verify`) which were not
examined here — doing one and not the others would trade one inconsistency for
another.

Nothing hid this: the fake verifier has carried a `returnEr` field since it was
written and every test set it to `nil`, so no test ever reached the cosign
failure path. Pinned now in
`TestVerifyArtifactSignature_CosignFailureIsNotPermissionDenied`, which fails
when the gap closes so whoever closes it is sent back here.

## `sign --file` signs as it validates, so a bad list signs part of it

`signExactFiles` checks each file exists at the point it signs it, inside one
loop. A list whose second entry is missing therefore signs the first and *then*
refuses:

```
signed = [dist/present.json]
err    = sign file "dist/missing.json" is missing or not a regular file: missing input
```

Nothing consumes a half-signed directory today — the command exits non-zero and
the publish step does not run — so the practical effect is a stray `.asc` beside
a file that was going to be signed anyway. It is recorded because the sibling
paths reviewed alongside it all validate their whole input before acting, and
because a signature is the one artefact where "produced during a failed run" is
worth being deliberate about.

The change is a validation pass over `files` before the signing pass. Pinned as
it behaves today in
`TestSign_ExactFileMissingRefusesBeforeSigningAnything`.

## An unrecognised `--fail-on-severity` narrows the gate instead of failing

`security scan dependencies --fail-on-severity` accepts `low`, `moderate`,
`high` or `critical`. Anything else warns and falls back to `CRITICAL`:

```
::warning::Unknown severity "medium", defaulting to critical
```

The fallback is in the unsafe direction. A caller who asked for a wide gate and
mistyped it gets the narrowest one, so HIGH and MEDIUM findings stop blocking
the release — and the only signal is one warning line in a CI log.

Two plausible spellings both land there:

| Input | Known | Filter used |
|---|---|---|
| `moderate` | yes | `CRITICAL,HIGH,MEDIUM` |
| `medium` | **no** | `CRITICAL` |
| `CRITICAL,HIGH` | **no** | `CRITICAL` |

`medium` is what trivy itself calls that band. `CRITICAL,HIGH` is worse: it is
the grammar the *sibling* command documents for the identical flag name —
`security scan container --fail-on-severity` takes a comma-list ("comma-list,
e.g. 'CRITICAL,HIGH'; narrow to 'CRITICAL' to relax"), while `scan
dependencies` takes a single word. A consumer who learns one command and
copies the spelling into the other silently loses coverage.

Two directions to consider, neither taken here:

- **Refuse an unknown value** (`ErrUsage`) instead of warning. A misconfigured
  gate is a configuration error, and failing closed is the safer reading for a
  security control.
- **Accept both grammars**, so the two subcommands stop disagreeing about what
  the same flag means.

The existing app-layer test covers the warning and the CRITICAL default, so the
behaviour is deliberate and tested; what is recorded here is the direction of
the fallback and the collision between the two subcommands. The two plausible
inputs are now rows in `TestMapTrivyFailSeverity`, where someone changing this
will see them.

## A camelCase Android product flavor produces a task gradle does not have

`capitalizeFirst` upper-cases the first letter and lower-cases everything after
it — faithfully, as its comment says, "same shape as the bash awk substr trick".
Gradle does not do the second half: it capitalises the first character of the
flavor and preserves the rest.

| Flavor | This code builds | Gradle expects |
|---|---|---|
| `fdroid` | `assembleFdroidRelease` | `assembleFdroidRelease` ✓ |
| `FDroid` | `assembleFdroidRelease` | `assembleFDroidRelease` |
| `proDemo` | `assembleProdemoRelease` | `assembleProDemoRelease` |

Lowercase flavors — the common case, and the only ones in the examples — are
unaffected. A camelCase flavor gets a task name that does not exist, and the
build fails at gradle with "task not found" rather than anywhere near the
cause.

The fix is to drop the `strings.ToLower` on the tail. It is recorded rather
than made because the lower-casing is deliberate bash-compatibility, and an
adopter whose flavor is `FDroid` may already have worked around it by declaring
the flavor lowercase. Pinned in `TestResolveAndroidBuildTasks`.

Adjacent, and lower stakes: `BuildTypes` is matched with `strings.Contains`, so
a misspelling such as `relase` matches neither `debug` nor `release` and yields
an empty task list rather than a refusal. Nothing is built and the run fails one
step later, in `AndroidGradleBuild`, which does refuse an empty task list
(`ErrUsage`). The message names the empty tasks rather than the typo that caused
them.

## A CRLF `gradle.properties` breaks `android version-info`

`ParseGradleVersionFromProperties` splits on `"\n"` and neither trims the line
before testing the prefix nor trims the value after it. Four consequences,
all probed:

| Input | version | version-code |
|---|---|---|
| `versionName=1.2.3\nversionCode=42\n` | `1.2.3` | `42` |
| `versionName=1.2.3\r\nversionCode=42\r\n` | `1.2.3\r` | `42\r` |
| `  versionName=1.2.3\n` (indented) | `unknown` | `unknown` |
| `versionName=1.2.3  \n` | `1.2.3  ` | `unknown` |
| `versionName=1.0\nversionName=2.0\n` | `2.0` (last wins) | `unknown` |

The carriage-return row is the damaging one. Both line-oriented sinks refuse a
scalar containing `\r`, so on a CRLF-checked-out Android project the command
does not merely report a wrong version — it fails:

```
ghaoutput: scalar output "version" contains a newline; use SetMultiline: validation failed
```

with an empty `$GITHUB_OUTPUT` and a message pointing nowhere near the cause.
Confirmed end-to-end against the real sink in
`TestAndroidVersionInfo_CRLFPropertiesFailOnARealSink`.

The indented row is a silent wrong answer rather than a failure: the project
reports version `unknown`, which then flows into the artifact names.

**One change fixes all of them** — trim the line before the prefix test and the
value after it, which is exactly what the sibling `gradleProperty` in
`internal/app/build/gradle.go` already does for the same file format. That also
resolves the last-wins/first-wins disagreement between the two readers if the
loop breaks on match.

Recorded rather than made because it changes values that are emitted as job
outputs and baked into artifact names, and because the two parsers should
probably become one — which is a slightly larger call than the trim itself.
Every row above is pinned in `TestParseGradleVersionFromProperties`, so a fix
will fail these tests and bring whoever makes it back to this entry.

## `materialize-build-secrets` fails with more than one build secret

`secret-mounts` is a newline-separated list of `id=NAME,src=PATH` entries,
emitted through the scalar `sink.Set`. Both line-oriented sinks refuse a scalar
containing a newline — it would forge further entries in their key=value files
— so the command works with one declared build secret and fails with two.

Proved against the real GitHub Actions sink, not the fake:

| Secrets | Result | `$GITHUB_OUTPUT` |
|---|---|---|
| 1 | ok | `secret-mounts=id=a,src=/…/a` |
| 2 | `emit secret-mounts: ghaoutput: scalar output "secret-mounts" contains a newline; use SetMultiline: validation failed` | *(empty)* |

The failure lands after the secret files are written to disk at 0600, so a
failed run leaves them behind with nothing naming them.

**Why this is not a one-line fix.** The error message recommends `SetMultiline`,
and that does work for `ghaoutput` (heredoc form, decoded back to the same
multi-line value). But `gitlaboutput.SetMultiline` returns `ErrUnsupported`
outright — a dotenv file has no multi-line form — so that change fixes GitHub
and Forgejo and leaves GitLab failing with a different error. Nor can the
separator simply become a comma: `id=NAME,src=PATH` already contains one, and
space is unsafe because a path may contain spaces.

So the real question is how a multi-valued output crosses the sink boundary at
all on a dotenv-only forge — `ManifestSink` is what `gitlaboutput` points at,
and whether this output should move to it is a contract decision.

Pinned as it behaves today in
`TestMaterializeBuildSecrets_MultipleSecretsFailOnARealSink`.

## The fake output sink is more permissive than every sink it doubles

The bug above survived because `fakeoutputsink.Set` accepts any value, while
both `ghaoutput` and `gitlaboutput` reject `\r` and `\n` in a scalar. Every
test in the repository that emits an output does so through the fake, so no
test can observe a value that the real adapters would refuse.

Tightening the fake to match was tried and immediately caught the
`secret-mounts` case above — the only one in the suite. It was reverted rather
than kept: with the defect unfixed, the guard turns two otherwise sound tests
(file materialisation, name splitting) into assertions about a bug, which
obscures what they exist to prove.

The guard is three lines and should go in as soon as `secret-mounts` is
resolved, so this class cannot reappear:

```go
if strings.ContainsAny(value, "\r\n") {
    return fmt.Errorf("fakeoutputsink: scalar output %q contains a newline; use SetMultiline: %w", key, errs.ErrValidation)
}
```

Related: `ParseGradleVersionFromProperties` can produce exactly such a value —
see the entry below.

## A missing package.json exits two different ways

Two functions in `internal/app/build/npm.go` read the same file:

- `readNPMPackageJSON` maps `fs.ErrNotExist` to `errs.ErrMissingInput`
- `npmHasScript` returns the raw `os.ReadFile` error, unwrapped by any sentinel

So `reusable-ci build npm application` in a directory without a package.json
exits with the generic failure code, while `build npm metadata` and `build npm
pack` in that same directory exit per `ErrMissingInput`. Same cause, same
package, different exit code — and exit codes are the part of the contract a
workflow branches on.

The malformed-JSON case is consistent between them (`ErrInvalidConfig`); only
the not-found case diverges.

The fix is to give `npmHasScript` the same `fs.ErrNotExist` mapping its sibling
already has. Recorded rather than made because it changes an exit code that a
consumer may be matching on, and because the two could equally be converged by
having `npmHasScript` call `readNPMPackageJSON` — a slightly larger refactor
with the same outcome. Pinned as it behaves today in
`TestNPMApplication_RejectsUnreadablePackageJSON`.

## `XcodeListBuiltArtifacts` cannot see an .xcarchive

The walk returns early on every directory:

```go
if d.IsDir() {
    return nil
}
```

and `.xcarchive` is a bundle *directory* — `Info.plist`, `Products/`, `dSYMs/`
inside. So the extension test below it never sees one, and the walk descends
into the bundle listing whatever `.ipa` files it finds there instead (normally
none).

Probed against the shape xcodebuild actually writes:

```
Built artifacts:
No artifacts found
```

This bites hardest on the unsigned path, whose only output *is* the archive:
`XcodeReleaseBuild` with `EnableCodeSigning: false` runs `archive` and no
export, so a build that fully succeeded reports no artifacts.

The existing test did not catch it because its fixture wrote
`build/app.xcarchive` as a plain file — a shape xcodebuild never produces. That
is the only reason the assertion held. The fixture is now realistic and the
behaviour is pinned as-is in `TestXcodeListBuiltArtifacts_SkipsArchiveBundles`.

The fix is to test the extension before the `IsDir` return, and to skip
descending into a matched bundle. It is recorded rather than made because the
output is a human-facing listing rather than data anything consumes, so nothing
is broken downstream by it being wrong — and because whether an archive should
count as a "built artifact" alongside a shippable `.ipa` is a presentation
choice.

## A failed Xcode archive still publishes its job outputs

`XcodeReleaseBuild` resolves and emits metadata before it archives. When the
archive is refused — `archive: scheme is required` with `ErrUsage` — the sink
already holds `ipa-name`, `version` and `build`, naming an IPA that was never
produced.

Confirmed by probe: `calls=[] keys=[build ipa-name version]`.

In practice a failed step fails the job, so a later step would have to opt in
with `if: always()` to read them. That makes this a smaller version of the same
question the ledger signing entry raises: a refused run should ideally leave
nothing behind, and the sibling commands reviewed alongside this one
(`GoMetadata`, `MavenReleaseBuild`, `NPMReleaseBuild`, `AndroidReleaseBuild`)
all now assert exactly that. Xcode is the one that does not.

Moving the emission after the archive is a small reordering. It is recorded
rather than made because emitting metadata early may be deliberate — a workflow
that names an upload artifact from `ipa-name` under `if: always()` would break
if the output disappeared on failure — and that is a workflow-contract question,
not a code one. Pinned as-is in `TestXcodeReleaseBuild_ArchiveErrorPropagates`.

## The Build SBOM summary says "release blocked" when nothing is blocked

`appsummary.BuildSBOMStatus` has two branches. Success names the bom file;
everything else prints:

```
- ✗ Generation step did not succeed — release blocked
```

on the reasoning, in its own comment, that "The SBOM step is mandatory — a
non-success outcome means the workflow has already failed before this summary
block ran".

That premise does not hold for any of the four release-build callers. Gradle,
npm, cargo and maven all treat the Build SBOM as best-effort: they catch the
error, print `WARN: ... SBOM generation failed (continuing)`, set the outcome to
failure, and **return nil**. The release proceeds.

Worse, `outcomeSkipped` takes the same branch. Probed on gradle:

| Input | `err` | Summary line |
|---|---|---|
| `EnableBuildSBOM: false` | `nil` | `✗ … release blocked` |
| `EnableBuildSBOM: true`, no tool version | `nil` | `✗ … release blocked` |

So turning the Build SBOM off — a supported, deliberate configuration — reports
a blocked release in the job summary of a release that completed. And a genuine
generation failure is indistinguishable from that deliberate choice, which is
the one case an operator would actually want to see.

Three outcomes are already modelled (`outcomeSuccess`, `outcomeSkipped`,
`outcomeFailure`) and the summary collapses two of them. Splitting the branch is
small and local to `buildsbom.go`. It is recorded rather than done because the
right words for each case are an operator-communication decision, and because
the comment asserting the step is mandatory suggests the summary may be the
correct half and the four best-effort callers the mistaken one — which is the
opposite change, and a much larger one.

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

## Two more unreachable guards

Alongside the `readGoModulePath` case below, two guards cannot fire.

`AndroidWriteSecretsProperties` refuses a secret that decodes to nothing:

```go
if len(body) == 0 {
    return fmt.Errorf("SECRETS_PROPERTIES_BASE64 decoded to zero bytes: %w", errs.ErrValidation)
}
```

The function opens with `if strings.TrimSpace(in.Base64) == ""` → skip, and the
decode strips all whitespace via `strings.Fields`. So every input that could
decode to zero bytes has already been skipped, and every input that gets past
the skip either fails to decode or yields at least one byte. Probed across
`""`, `" "`, `"\n"`, `"\t \n"`, `"="`, `"===="`, `"AA=="`: the guard is never
reached.

This one is worth more than a note, because a test asserted the opposite
behaviour and passed. `TestAndroidWriteSecretsProperties_RejectsEmptyDecoded`
was named for the refusal, carried a comment saying "Empty body after decode is
treated as no secrets configured (skip)", and asserted `err == nil` — three
different beliefs in one test, held together by a fixture
(`base64.StdEncoding.EncodeToString(nil)`, which is `""`) that reaches the skip
branch and never the guard. It has been replaced by
`TestAndroidWriteSecretsProperties_NoSecretIsASkip`, which covers the branch
that does run and says why the other cannot.

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
