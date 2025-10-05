# Development Guide

## Prerequisites - Linux

1. Install [mise](https://mise.jdx.dev/) (manages linting tools):

   ```bash
   curl https://mise.run | sh
   ```

2. Activate mise in your shell:

   ```bash
   # For bash - add to ~/.bashrc
   eval "$(mise activate bash)"

   # For zsh - add to ~/.zshrc
   eval "$(mise activate zsh)"

   # For fish - add to ~/.config/fish/config.fish
   mise activate fish | source
   ```

   Then restart your terminal.
3. Install project tools, including the uv-backed REUSE checker and the `jq`
   test-fixture dependency:

   ```bash
   mise install
   ```

4. Run quality checks:

   ```bash
   just verify
   ```

## Prerequisites - macOS

1. Install [mise](https://mise.jdx.dev/) (manages linting tools):

   ```bash
   brew install mise
   ```

2. Activate mise in your shell:

   ```bash
   # For zsh - add to ~/.zshrc
   eval "$(mise activate zsh)"

   # For bash - add to ~/.bashrc
   eval "$(mise activate bash)"

   # For fish - add to ~/.config/fish/config.fish
   mise activate fish | source
   ```

   Then restart your terminal.
3. Install newer bash than macOS default:

   ```bash
   brew install bash
   ```

4. Install project tools, including the uv-backed REUSE checker and the `jq`
   test-fixture dependency:

   ```bash
   mise install
   ```

5. Run quality checks:

   ```bash
   just verify
   ```

## Available Commands

Run `just` to see all available commands.

## Local verification tools

Project contributors get the repository's pinned toolchain with `mise install`.
For ad-hoc verification outside that toolchain, mise's aqua backend can install
the portable release verifiers:

```bash
mise use -g aqua:sigstore/cosign
mise use -g aqua:cli/cli             # gh
mise use -g aqua:containers/skopeo
```

`gpg`, `git`, `sha256sum`, and `ssh-keygen` come from the host distribution.
Check `cosign version`, `gh --version`, and `skopeo --version` before diagnosing
a verification failure as an artifact problem.

### Local Git signature setup

SSH verification needs an `allowed_signers` file whose principal is the signer
email and whose key material comes from a trusted, independently reviewed
source. GitHub exposes public SSH keys at
`https://github.com/<username>.keys`; it exposes public OpenPGP keys at
`https://github.com/<username>.gpg`. Confirm identity/fingerprints out of band
before trusting either endpoint.

```bash
mkdir -p ~/.ssh
curl -fsS https://github.com/<username>.keys -o /tmp/signer.keys
while IFS= read -r key; do
  printf '%s %s\n' 'developer@example.com' "$key"
done < /tmp/signer.keys >> ~/.ssh/allowed_signers
git config --global gpg.ssh.allowedSignersFile ~/.ssh/allowed_signers

curl -fsS https://github.com/<username>.gpg -o /tmp/signer.gpg
gpg --show-keys /tmp/signer.gpg       # verify fingerprint first
gpg --import /tmp/signer.gpg
```

Repository release policy uses the reviewed files under `.reusable-ci/`, not a
developer's global file. The setup above is only for local inspection with
`git verify-tag`, `git verify-commit`, and `git log --show-signature`.

References: [Git signing](https://git-scm.com/book/en/v2/Git-Tools-Signing-Your-Work),
[Sigstore](https://docs.sigstore.dev/), [SLSA](https://slsa.dev/), and
[GitHub Actions hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions).

## Where code goes

`internal/` is ports and adapters. Every import points inward at `domain`:

```text
cli  ──►  app  ──►  domain  ◄──  adapters
```

| You are writing | It goes in |
|---|---|
| A business rule, or the interface a rule needs | `internal/domain/…` |
| The ordering of steps in a use case | `internal/app/…` |
| A wrapper around a real tool, API, or the environment | `internal/adapters/…` |
| Flag parsing, output rendering, adapter construction | `internal/cli/…` |
| A helper with no domain knowledge (`listval`, `safeexec`) | `internal/<name>` |

The layers are siblings on disk because the layering is expressed by import
direction, not by nesting. Two rules cover most decisions:

- **Need a tool from a use case?** Do not import the adapter. Declare a small
  interface listing only the methods you need, next to the code that uses it
  (`gitOps` in `internal/app/validate/tags.go` is the reference example), and
  construct the real adapter in `internal/cli`.
- **Adapters never import `app` or `cli`,** and `domain` imports neither
  those nor `adapters`.

`internal/archguard` enforces this at test time and names the fix when it
fails. The full rule, including why `platform` is an adapter and why pure
OpenPGP verification lives in `internal/pgp` rather than an exempt adapter, is in
[ADR 0004](adr/0004-package-layering.md).

## Error classification

Every error that reaches `main` goes through `errs.ExitCodeFromError`. An error
carrying no `errs.Err*` sentinel falls through to `ExitCodeSoftware` (70),
whose meaning is *"an error we did not classify. File a bug against
reusable-ci"*. That default is correct, which is exactly why an unclassified
error is a defect: it tells an adopter who mistyped a path that our tool is
broken.

**Classify at the boundary that knows the meaning; add context above it
without re-classifying.**

| The boundary | Use | Because |
|---|---|---|
| Reading an operator-supplied file | `cliio.ReadFile` | Maps absent → `ErrMissingInput` (66), unreadable → `ErrPermissionDenied` (77), a directory → `ErrMissingInput`, and bounds the read |
| Reading below an already checked root | `cliio.ReadFileInRoot` | Preserves the same file-type and size bounds without reopening through the ambient filesystem |
| Running an external binary | `safeexec.WrapError` | A non-exit failure is the tool never having run, so `ErrDependencyUnavailable` (69), *"x not found in $PATH"*, not a crash |
| An HTTP response | `errs.FromHTTPStatus` | 4xx is the request being refused, not an outage |
| Parsing operator or tool data | `ErrInvalidConfig` (78) / `ErrMalformedInput` (65) | *Their* config vs *some tool's* output |

Two rules follow from it:

- **Attach the sentinel once.** Layers above add context with a single `%w`.
  Wrapping twice reads back as `"…: invalid configuration: invalid
  configuration"` in the one line an operator sees. `domain/config.invalidConfig`
  is the idempotent form when a helper cannot know whether its caller already
  classified.
- **One mistake, one exit code.** When two guards can catch the same operator
  error, they must agree. Comparing `Value == ""` while the layer below trims
  meant `--sboms ""` and `--sboms "   "` exited differently.

`container ledger merge` classifies a missing local input path as
`ErrMissingInput` (66), including a path disappearing during discovery. This
classification belongs at the local-input boundary; network failures still map
to dependency unavailable (69).

Plan lookup and merging share `planfile.Decode`: plan and scope containers must
be JSON objects, and JSON number spellings are retained without float64 conversion.
`plan write` stores the whitespace-normalized command scope it validated.

Environment-sourced credentials remove trailing CR/LF only. Leading/interior
whitespace and explicit token flags are unchanged; this is not general token
sanitization. Default keyless verification identity follows the execution runner,
independently of the forge selected for API operations.

Repository sync guards read governed inputs with `reporoot.ReadFile` and
`reporoot.ReadDir`, rejecting symlink components instead of following them.
Generated-file failures report bounded difference context, sizes, hashes and the
refresh command. Artifact vocabulary checks use named documentation blocks;
mentions elsewhere in prose cannot satisfy them.

The shell-source guard discovers `.sh`, `.bash`, `.sh.tmpl`, and extensionless
files with a supported `sh`/`bash` shebang. It skips Git metadata, dependency and
build-output directories. Embedded workflow/Go/just recipes are separate scopes;
the existing printf rule is a lexical alarm, not a complete shell parser.

Note the sentinels are project-chosen, not BSD `sysexits` throughout:
`ErrUsage` is **2** (POSIX CLI convention), not 64. `errs.ExitCodeConstants`
and its test are the reference.

`internal/archguard` enforces the exec-adapter half at test time. The rest is
enforced by tests asserting the sentinel: `errors.Is(err, errs.ErrX)`, never
a bare `err != nil`, which passes whatever the classification turns into.

## Test discipline

The pipeline's verdict is definitive: passing means deploy, failing means
fix. That contract only holds if individual tests are reproducible, so the
codebase follows the standard non-determinism discipline:

- **No flaky tests.** A failure is either a real bug or a missing test
  guarantee; "just re-run it" is not an acceptable fix. Quarantine the
  test (`t.Skip("…tracking <issue>")`) until it is fixed or deleted:
  never leave a `// flaky` comment behind a green build.
- **Time is injected.** Anywhere a test output depends on the wall clock,
  the production code takes a `now func() time.Time` (see
  `internal/app/summary/*.go`, `internal/app/build/go.go::resolveBuildDate`,
  `internal/app/security/trivy.go::reportTimestamp`). Tests pass `fixedNow()`.
  When `SOURCE_DATE_EPOCH` is set, the same injection point honours it,
  making reproducible-build callers and tests share the same code path.
- **Per-test filesystem state.** `testfs.NewReal(t)` returns a fresh
  `t.TempDir()`-rooted workspace per test. No `os.Chdir` outside the
  helper. No state survives between tests in the same package.
- **External tools must skip cleanly, not flake.** Integration tests
  that exercise real `cargo` / `mvn` / `gpg` / `syft` use
  `requireTool(t, "cargo")` which calls `t.Skip` when the tool is
  absent. A missing toolchain is a skip; a hung subprocess is a bug.
- **`t.Parallel()` is the default**, both for unit and integration tests.
  Anything that can't run in parallel needs an explanatory comment.

Pipeline-level determinism is documented in `docs/verification.md#deterministic-pipeline`.

## Workflow Refactor Checklist

When changing workflows or workflow-facing binary behavior:

1. Keep public workflow contracts stable unless the change is explicitly versioned.
2. Follow `docs/workflow-design-policy.md` for workflow structure and binary command rules.
3. Run `actionlint .github/workflows/*.yml`.
4. Parse workflow YAML and check reusable-workflow input compatibility.
5. Run `bash -n` for touched bootstrap scripts.
6. Add or update Go tests for behavior changes. For standalone shell
   scripts, keep `bash -n`, `shellcheck`, and targeted Go black-box tests
   next to the scripts when needed.

## Branch Testing Reusable Workflows

Consumers can run the reusable workflow stack from an unreleased reusable-ci
branch, but only when the workflow ref, runtime images, and plain-runner binary
ref all point at the same reusable-ci revision. Calling only
`.../.github/workflows/<workflow>.yml@<branch>` is not enough because workflow
input defaults intentionally track the released runtime image line, not your
feature branch.

GitHub resolves nested reusable workflows that use
`uses: ./.github/workflows/<name>.yml` from the same commit as the workflow that
called them. Those local nested calls are safe for branch testing and do not need
an explicit `@<ref>`.

For full branch execution from a consumer repository:

1. Push the reusable-ci branch to `github.com/diggsweden/reusable-ci`.
2. Run `Self Runtime Container` on that branch with `publish=true`.
3. Use the resulting `:sha-<short-sha>` runtime image tags for every runtime
   image family the called orchestrator accepts.
4. Call the reusable workflow at the same commit SHA.
5. Set `reusable-ci-binary-ref` to the same commit SHA.

Example release-orchestrator branch call:

```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@abcdef0123456789abcdef0123456789abcdef01
    with:
      branch: main
      artifacts-config: .reusable-ci/artifacts.yml
      reusable-ci-binary-ref: abcdef0123456789abcdef0123456789abcdef01
      runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-base:sha-abcdef0
      runtime-image-full: ghcr.io/diggsweden/reusable-ci-runtime:sha-abcdef0
      runtime-image-java: ghcr.io/diggsweden/reusable-ci-runtime-java-25:sha-abcdef0
      runtime-image-node: ghcr.io/diggsweden/reusable-ci-runtime-node-24:sha-abcdef0
      runtime-image-rust: ghcr.io/diggsweden/reusable-ci-runtime-rust-stable:sha-abcdef0
      runtime-image-android: ghcr.io/diggsweden/reusable-ci-runtime-android-35:sha-abcdef0
```

Example pullrequest-orchestrator branch call:

```yaml
jobs:
  pullrequest:
    uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@abcdef0123456789abcdef0123456789abcdef01
    with:
      project-type: npm
      reusable-ci-binary-ref: abcdef0123456789abcdef0123456789abcdef01
      runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-base:sha-abcdef0
```

The `:sha-<short-sha>` value is the seven-character commit prefix from the
reusable-ci branch build. It must match the commit used by the workflow ref and
`reusable-ci-binary-ref`; otherwise the YAML, containerized binary, and
plain-runner Go install can come from different revisions.

Remaining branch-testing caveats:

- Defaults point to the released runtime image line for normal usage. Branch
  tests must override the relevant runtime image inputs. Branch refs work for
  quick iteration, but commit SHAs avoid drift after `:sha-<short-sha>` images
  are published.
- Plain Ubuntu/macOS jobs install with
  `go install github.com/diggsweden/reusable-ci/v3/cmd/reusable-ci@<ref>`, so the
  `reusable-ci-binary-ref` branch or SHA must exist in the upstream reusable-ci
  repository.
- Fork-only branches are not fully supported by the current public contract.
  Push the branch to upstream reusable-ci or add an explicit source-repository
  input before testing from forks or mirrors.
