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
