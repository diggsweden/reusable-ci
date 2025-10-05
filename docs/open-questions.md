# Open questions from the test review

> **Active decision register.** Keep an item here until it is resolved or its
> durable security/architecture home records the decision and evidence.
> This file is current documentation, not an attic record.

This register contains three unresolved architecture or security questions.
Resolved findings are removed; Git history records their prior analysis.

## Current questions

- [Signing zero packages succeeds](#signing-zero-packages-succeeds)
- [Path-safety policies remain context-specific](#path-safety-policies-remain-context-specific)
- [Subprocess secret isolation still relies on denylists](#subprocess-secret-isolation-still-relies-on-denylists)

## Signing zero packages succeeds

`GPGSignPackages` returns `count = 0` and prints "GPG-signed 0 package(s)" when
the directory holds no `.deb`, `.rpm` or `.apk`.

This is harder to call than it first looks, and the answer depends on a gap
elsewhere:

- **Nothing in this repository calls `release gpg sign-packages`.** It appears
  in no workflow and no script, only in the generated CLI reference, while its
  siblings `gpg import` and `gpg cleanup` are invoked from release workflows.
- **It is not dead surface.** The release path collects `.deb`, `.rpm` and
  `.apk` as publishable assets and publishes their `.sig` sidecars, and this
  command is the only thing that produces those sidecars. A consumer whose
  build emits distro packages gets them published unsigned unless it invokes
  the CLI directly.
- **There is nothing to gate a step on.** Neither `artifacts.yml` nor the config
  plan declares that a project produces distro packages. A publish-stage step
  could only be gated on GPG signing being enabled, which is true for releases
  that produce no packages. Under that design, succeeding on zero is correct.

The open question is whether `artifacts.yml` should declare distro-package
outputs. That would give the publish stage a meaningful gate and make an empty
package set a policy failure when packages were promised.

## Path-safety policies remain context-specific

`internal/pathsafe.Relative` holds the shared workspace-relative component
rule, and release-plan, release-file, version-bump, and release-images paths use
it. Six context-specific guards remain:

| Where | Additional contract |
|---|---|
| `domain/artifact.SafeJoin` | Validates and joins under a root; also rejects backslash separators. |
| `app/validate.safeWorkingDir` | Defaults, cleans, and returns a working directory. |
| `domain/config.validateWorkingDirectory` | Returns artifacts.yml violation strings and rejects shell-style references. |
| `app/release.collectAttachmentAssets` | Validates caller-owned glob patterns before assembly. |
| `app/release.expandAttachmentPatterns` | Repeats the attachment-glob rule before upload. |
| `app/container.recreateSignerMetadataDir` | Deliberately rejects any `..` substring before `RemoveAll`, which is stricter than the component rule. |

These are not mechanical duplicates: they have different return types, error
classes, defaults, and joining or glob semantics. The two attachment guards are
the closest pair. `SafeJoin` and the signer metadata guard intentionally answer
stricter questions than `pathsafe.Relative`.

The architecture question is whether the common component rule should be
exposed in forms that preserve those caller-specific contracts, or whether the
remaining implementations are clearer where they are. A source-pattern guard
was rejected because it could not detect the separator bug that motivated the
shared helper without producing a broad allowlist and false confidence.

## Subprocess secret isolation still relies on denylists

Three subprocess boundaries maintain hand-written environment denylists:

| Policy | Applies to |
|---|---|
| `signerSecretEnv()` | Syft and skopeo subprocesses used around signer/image-evidence paths. |
| `changelogEnv` | The changelog renderer. |
| `miseInstallEnv` | Mise token lookup and empty package-index overrides. |

Their memberships differ for legitimate reasons. Mise controls its own token
selection rather than acting as a general secret boundary, while the other
tools need different runtime inputs and syft/skopeo may need registry access.
The shared risk is that a denylist must be updated for every future credential.

The GPG and cosign adapters use the safer neighboring model: a minimal runtime
allowlist plus explicitly named inputs. Converting the remaining security
boundaries requires an evidence-based list of what each subprocess needs,
including registry authentication, proxies, and TLS roots. Guessing too narrowly
would fail only during a release, so that policy decision remains open.
