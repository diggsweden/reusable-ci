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
3. Install pipx (needed for reuse license linting):

   ```bash
   # Debian/Ubuntu
   sudo apt install pipx
   ```

4. Install project tools:

   ```bash
   mise install
   ```

5. Run quality checks:

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

4. Install pipx (needed for reuse license linting):

   ```bash
   brew install pipx
   ```

5. Install project tools:

   ```bash
   mise install
   ```

6. Run quality checks:

   ```bash
   just verify
   ```

## Available Commands

Run `just` to see all available commands.

## Test discipline

The pipeline's verdict is definitive — passing means deploy, failing means
fix. That contract only holds if individual tests are reproducible, so the
codebase follows the standard non-determinism discipline:

- **No flaky tests.** A failure is either a real bug or a missing test
  guarantee; "just re-run it" is not an acceptable fix. Quarantine the
  test (`t.Skip("…tracking <issue>")`) until it is fixed or deleted —
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

1. Push the reusable-ci branch to `github.com/diggsweden/reusable-ci/v3`.
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
