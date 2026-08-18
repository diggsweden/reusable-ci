# Open questions from the test review

Behaviours found while reviewing the test suite that look deliberate enough not
to change unasked, but odd enough to be worth a decision. Each was confirmed by
running the code, not by reading it. None is fixed.

Security-shaped findings live in [Threat Model](threat-model.md) and are listed
at the bottom of this page rather than repeated.

## 1. A container SBOM is generated once per artifact type, identically

`GenerateContainer` builds one `GenerateInput` and passes it unchanged on every
iteration of the artifact-type loop. The type reaches the log line and nothing
else. With `--artifact-types "maven,npm"` syft is invoked twice with the same
image and the same two output paths, so the second scan overwrites the first
result.

Recorded by the multiple-artifact-types test in
`internal/app/sbom/generatecontainer_test.go`, which asserts both calls as they
are so the duplication is visible rather than implied by a count.

Container scanning is not cheap, so this is duplicated wall-clock on every
release declaring more than one artifact type. Either the loop should vary
something per type, or the SBOM should be generated once and the loop should go.
Deciding which needs to know what the per-type SBOM was originally meant to
contain.

## 2. `checksums --output` creates the parent directory in one mode and not the other

The assembly path calls `os.MkdirAll(filepath.Dir(outputFile))`. The
non-assembly path opens the manifest with `O_CREATE`, which does not create
parents, so `--output some/dir/file` fails with "no such file or directory"
when `some/dir` does not exist. The same flag behaves differently depending on
whether `--assembly` was passed.

Noted in `TestChecksums_HonoursACustomOutputPath`, whose nested row creates the
directory first — a precondition that read as fixture noise until the asymmetry
was found.

Making the non-assembly path create it too is one line and matches the flag's
generated description, which says nothing about the directory having to exist.

## 3. Signing zero packages succeeds

`GPGSignPackages` returns `count = 0` and prints "GPG-signed 0 package(s)" when
the directory holds no `.deb`, `.rpm` or `.apk`. A release configured to sign
distro packages that produced none therefore passes the signing step.

The comparable case in `Checksums` is deliberate and differs: it *writes* an
empty manifest so downstream steps can rely on the file existing. Signing
nothing produces nothing, so there is no equivalent reason. Whether this should
be an error, a warning, or silence depends on whether a release is ever
expected to declare package signing and legitimately produce no packages.

## 4. An empty `revision` label ships on the image

When the event context carries no SHA, `BuildLabels` emits
`org.opencontainers.image.revision=` rather than omitting the label. The
published image then carries an empty revision claim.

Surfaced by comparing the label set exactly in
`TestComputeMetadata_MissingFieldsComeFromTheForge`, which now records it.

Omitting a label whose value is unknown is the usual OCI convention; emitting it
empty is a claim that the revision is the empty string.

## 5. A refused transfer plan can leave downloads behind

`DownloadArtifacts` validates each item inside the download loop, so a plan
whose first item is valid and whose second is invalid downloads the first before
failing. Validating the whole plan up front would make a refusal mean nothing
happened.

The path-safety half of this is in the threat model; the ordering half is a
plain behaviour question and sits here.

## 6. `internal/livetest` test build is broken

`go test ./...` cannot pass repo-wide:

```
internal/livetest/trust_internal_test.go:203:12: undefined: requestErr
```

`go build ./internal/livetest/` succeeds, so this is test-file only. It is
unrelated to the test review — the package has a large uncommitted working tree
— but it means the repo-wide suite has been red throughout.

## Recorded in the threat model

- Profile-dependent `externalParameters` reserved keys — a caller can declare
  `source` under the forgejo profile, where the engine does not compute it and
  so does not reserve it.
- Artifact transfer plan path validation — `DownloadArtifacts` does not check
  the item path that decides where files land, while `AssembleDist` checks the
  same field on the same plan.
- `assemble-dist --path` deletes without a path check — `--prune-dirs` runs
  `os.RemoveAll` over a path validated only for being non-empty and
  single-line, while its two neighbours use `validateSafeRelativePath`.
- Vacuous OCI release identity match — an empty expected identity matches an
  image carrying no labels.
