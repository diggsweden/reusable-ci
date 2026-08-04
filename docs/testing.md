<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Testing conventions

Test architecture for the Go runtime. Follow these patterns when adding new
code; PR review checks that the layers are respected and the helpers are
used.

> Current testing policy for the Go codebase.

## Layers

Tests sit in one of four buckets. Each has a different shape, build tag,
and CI gate.

| Layer | What it tests | Build tag | Speed | Parallel | Network / CLI |
|---|---|---|---|---|---|
| **domain** | pure logic — parsers, transforms, decisions | (default) | <50ms | yes | no |
| **adapter** | I/O — real CLIs (`gpg`, `git`, `trivy`), real HTTP | `!short` for slow ones; `integration` for full-stack | <2s | yes (per package) | yes |
| **CLI / e2e** | the binary as a black box | `e2e` | <5s | no | builds binary |
| **live / conformance** | the same scenario against every real forge | `live` | minutes | no (`-p 1`) | a real lab |

Run them as:

```text
go test ./...                          # domain + fast adapter
go test -tags=integration ./...        # + full-stack adapter
go test -tags=e2e ./cmd/...            # + e2e binary smoke
```

### The live tier

`internal/livetest/` holds a kit and `internal/livetest/conformance/` the
scenarios (`PAR-*`). One scenario body runs against every forge that *claims*
the capability it needs, so parity is enforced by shape rather than by
discipline — the alternative, a suite per forge, is how parity rots.

It exists because every other layer verifies an adapter against a fake written
from the same assumptions as the adapter, so an assumption that is wrong is
wrong in both places and both agree. Only a real forge breaks that tie. It is
what found GitLab's missing delete-then-create on re-release, the registry
adapter reporting permanent failures as retryable, and that Forgejo drops
untagged manifests while GitLab keeps them.

Run it against a disposable lab only:

```text
just test-live      # preflights the contract, builds once, revokes tokens on exit
```

Scenarios are repeatable by construction, and the suite is verified that way
rather than assumed to be: `NewScratchRepo` deletes **before** it creates, so a
run that died between create and cleanup cannot make "start from empty" a lie;
anything a forge scopes to the *owner* rather than the repository (packages) is
swept explicitly and versioned per run, because deleting the repository does not
remove it; and fixtures pre-check that state is absent, so "it was published"
cannot be mistaken for "it was already published".

**Diagnosing a failing in-runner scenario.** A failing scenario prints the tail
of the job log itself, on both forges, because a bare conclusion is the same
defect as a missing tool discovered mid-run: right answer, useless vocabulary.

When the log is not enough — the job passed but produced the wrong artifact, or
the failure is in state teardown is about to delete — `RC_LIVE_KEEP_SCRATCH=1`
keeps the scratch repository so it can be inspected on the forge:

```text
RC_LIVE_KEEP_SCRATCH=1 go test -tags=live -run TestInRunner_... ./internal/livetest/...
```

It is an environment variable rather than a flag on purpose — awkward enough
that nobody leaves it on. Nothing accumulates either way: the next run deletes
the repository before recreating it, so what is kept is one generation.

It is **never in PR CI**: it is live, destructive, and human-invoked against
git-provider-lab. The recipe refuses to run without a valid target contract and
an explicit destroy confirmation naming the run.

In this repository's own CI, the self-validation workflow runs:

- `just test` (unit + integration)
- `just test-e2e`
- `just test-fuzz` (seed corpus only; not active mutation fuzzing)

## Tests live next to source

```text
internal/domain/tagrule/apply.go
internal/domain/tagrule/apply_test.go         ← unit tests
internal/domain/tagrule/parse_test.go         ← unit + property + fuzz
internal/adapters/github/repo.go
internal/adapters/github/repo_test.go          ← real `gh` (or mocked)
internal/adapters/github/release_integration_test.go   ← //go:build integration
internal/app/container/compute_metadata.go
internal/app/container/compute_metadata_test.go       ← stubbed deps
cmd/reusable-ci/e2e_test.go                   ← //go:build e2e
```

No product-level `tests/` directory. The legacy Bats harness is retired;
Go behavior tests sit next to the code they exercise, and the remaining
shell-native bootstrap surface is tested under `scripts/...` with Go tests.

## Black-box by default, `_internal_test.go` for the rest

New test files use `package foo_test` — a black-box test from outside
the package. This catches accidental coupling to private API, makes the
test file act as runnable documentation of the public contract, and is
what the rest of the codebase expects.

Carve-out: when a test legitimately needs to touch an unexported helper,
state machine, or sentinel, use `package foo` (same package as the
production code) and put it in a file named `<thing>_internal_test.go`.
The suffix makes the intent visible to reviewers without having to open
the file.

```text
internal/domain/parser/parse.go
internal/domain/parser/parse_test.go              ← package parser_test
internal/domain/parser/parse_internal_test.go     ← package parser
```

Keep the carve-out narrow: only the cases that *cannot* be tested from
outside. If you find yourself reaching for the carve-out, ask first
whether the missing access is a hint that the API needs widening
(elevate to exported) or a hint that the unit under test is too
coarse-grained (split it). The carve-out is the third option, not the
first.

## Test naming conventions

Outer test names follow `Test<Component>_<Method>_<Scenario>`:

```go
func TestApply_RawValue_EmitsLiteral(t *testing.T) { ... }
func TestApply_RefBranch_DoesNotFireOnTag(t *testing.T) { ... }
```

Subtest names use snake_case (the failure output reads the same as Go's
auto-translation of spaces, but grep finds them more reliably):

```go
t.Run("raw_value_emits_literal", func(t *testing.T) { ... })
```

## Table-driven everything

Each test is a slice of cases. Use `tests` for the slice and `testCase`
for the loop variable — they're consistent enough across the codebase
that any IDE rename refactors them in one pass. The struct fields are
`name` / `given` / `want` / `wantErr` / `errContains`:

```go
func TestApply_TagRules(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name  string
        given tagrule.Rule
        evt   provider.EventContext
        want  string
        fired bool
    }{
        {name: "raw_value_emits_literal", given: ..., want: "main", fired: true},
        {name: "ref_branch_does_not_fire_on_tag", given: ..., fired: false},
        // ...
    }
    for _, testCase := range tests {
        t.Run(testCase.name, func(t *testing.T) {
            t.Parallel()
            got, fired, err := tagrule.Apply(testCase.given, &testCase.evt)
            require.NoError(t, err)
            require.Equal(t, testCase.fired, fired)
            if testCase.fired {
                require.Equal(t, testCase.want, got)
            }
        })
    }
}
```

Use `testify/require` (not `assert`) — fail-fast on the first mismatch
gives a clear root-cause line instead of cascading errors. `t.Helper()`
on every test helper.

Failure output reads like spec lines: `--- FAIL: TestApply_TagRules/raw_value_emits_literal`.

This convention is partially adopted across the codebase (~5 packages
as of writing); older tests use ad-hoc shapes that are migrated
opportunistically. New tests should follow the convention from the start.

## Property + fuzz tests on parsers

Every parser gets:

- a `quick.Check` property that "input never panics" (5000 random inputs)
- a `Fuzz<Name>` test seeded with known good inputs for `go test -fuzz`

```go
func TestParse_NeverPanics(t *testing.T) {
    quick.Check(func(s string) bool {
        _, _ = tagrule.Parse(s)
        return true
    }, &quick.Config{MaxCount: 5000})
}

func FuzzParse(f *testing.F) {
    f.Add("type=raw,value=main,enable=true")
    f.Add("type=semver,pattern={{major}}.{{minor}},enable=true")
    f.Fuzz(func(t *testing.T, s string) { _, _ = tagrule.Parse(s) })
}
```

The repository's own CI runs the fuzz seed corpus via `just test-fuzz`.
Active mutation fuzzing remains a local or scheduled task (`go test -fuzz=Fuzz...
-fuzztime=30s`) per parser. Crashes get committed under
`testdata/fuzz/Fuzz<Name>/<seed>` as regression cases.

## Golden files

Use `internal/testutil/golden` for non-trivial outputs:

```go
got := metadata.AssembleLabels(...)
golden.EqualString(t, "labels.txt", got)
```

The fixture lives at `testdata/golden/labels.txt`. Refresh on intentional
change with `go test -update ./...`. CI runs without `-update` and fails
on diff; reviewer must approve every golden change.

## testutil packages

All test helpers live under `internal/testutil/`. Each package is small,
returns concrete types, and registers `t.Cleanup` itself — tests never
call `defer cleanup()`.

| Package | Purpose | Replaces |
|---|---|---|
| `golden` | Golden-file equality with `-update` flag | legacy shell partial-output assertions |
| `mockbinary` | Shell stubs on PATH that record argv/stdin/cwd | `create_mock_binary` |
| `isolatedgit` | Throwaway repo + isolated HOME, optional bare remote | `common_setup_with_isolated_git` + `init_remote_repo` |
| `testenv` | Canonical isolated env wrapper for env-sensitive tests | repeated `t.Setenv` + ad-hoc HOME/XDG isolation |
| `testfs` | Thin real-FS and memory-FS helper for file-heavy tests | repeated `t.TempDir` + `os.WriteFile` scaffolding |
| `gpgkey` | Throwaway ed25519 GPG key in isolated GNUPGHOME | per-test setup blocks |
| `ghaenv` | Tempfile-backed `GITHUB_OUTPUT`/`STEP_SUMMARY` with heredoc reader | `common_setup_with_github_env` + `get_github_output_multiline` |
| `glabenv` | GitLab-CI env (`CI_*`) + dotenv-style `CI_OUTPUT` reader | new GitLab test helper |
| `fakeprovider` | In-memory `provider.Provider` with call recorder | in-process provider fake |
| `fakeoutputsink` | In-memory `ci.OutputSink` capturing scalar + multiline | in-process output fake |
| `fakemanifestsink` | In-memory `ci.ManifestSink` capturing stage JSON bodies | in-process manifest fake |
| `fakegitserver` | `httptest.NewServer` with route registration for GitHub API | HTTP fake server |
| `fakegitlabserver` | Same for GitLab API (preserves `%2F` in project paths) | HTTP fake server |
| `fixtures` | `//go:embed`'d sample data + named getters | embedded test fixture files |

When a use case needs a new helper concern, add a package — don't grow
existing ones across responsibilities.

For env-sensitive tests, prefer `internal/testutil/testenv` directly or a
helper that composes it (`ghaenv`, `glabenv`, `isolatedgit`, `gpgkey`) so
HOME/XDG/git/GPG/proxy/locale isolation is applied consistently.

Tests that need a real working directory should prefer
`testfs.NewReal(t).Chdir()` over open-coded `os.Chdir` / `t.Chdir` blocks.
Because cwd is process-global, those tests must not call `t.Parallel()` at
that scope.

## Patterns by layer

### Domain — pure, parallel

```go
package tagrule_test

func TestApply_SemverMajorMinor(t *testing.T) {
    t.Parallel()
    rule := tagrule.Rule{Type: tagrule.TypeSemver, Attrs: ...}
    evt := provider.EventContext{RefType: provider.RefTypeTag, RefName: "v2.5.7"}
    got, fired, err := tagrule.Apply(rule, &evt)
    require.NoError(t, err)
    require.True(t, fired)
    require.Equal(t, "2.5", got)
}
```

No `t.Setenv`, no `os.MkdirAll`, no `exec.Command`.

### Adapter — real binaries / HTTP

```go
package github_test

func TestRepo_FetchMetadata(t *testing.T) {
    bin := mockbinary.New(t)
    bin.Add("gh", `cat <<JSON
{"description":"x","license":{"spdx_id":"Apache-2.0"}}
JSON`)

    p := github.New(github.WithGHPath(bin.Path("gh")))
    md, err := p.FetchRepoMetadata(t.Context(), "owner/repo")
    require.NoError(t, err)
    require.Equal(t, "Apache-2.0", md.LicenseSPDX)
    require.Len(t, bin.Invocations("gh"), 1)
}
```

Slower tests (~50ms+) get `//go:build !short`. Full-stack ones that
talk to `httptest.NewServer` plus a real CLI get `//go:build integration`.

### Application — stubbed deps

```go
package container_test

func TestMetadata(t *testing.T) {
    fp := fakeprovider.New(t).
        WithEventContext(provider.EventContext{
            RefType: provider.RefTypeTag, RefName: "v1.2.3",
        })
    fs := fakeoutputsink.New(t)

    err := container.Metadata(t.Context(),
        &deps.Deps{Provider: fp, OutputSink: fs},
        container.MetadataInput{
            ImageName: "ghcr.io/x/y",
            TagRules:  "type=semver,pattern={{version}},enable=true",
        })
    require.NoError(t, err)
    require.Equal(t, []string{"ghcr.io/x/y:1.2.3"}, fs.Multiline("tags"))
}
```

No subprocess, no network, no filesystem (except what `t.TempDir`
creates inside the helpers).

### CLI / e2e — binary as black box

```go
//go:build e2e

func TestCLI_ContainerMetadata_Smoke(t *testing.T) {
    bin := buildBinary(t) // helper that runs `go build` once per package
    cmd := exec.CommandContext(t.Context(), bin, "container", "metadata")
    cmd.Env = append(os.Environ(),
        "IMAGE_NAME=ghcr.io/x/y",
        "TAG_RULES=type=raw,value=main,enable=true",
        "GITHUB_OUTPUT="+t.TempDir()+"/out",
        // ...
    )
    out, err := cmd.CombinedOutput()
    require.NoError(t, err, string(out))
}
```

These are the fewest tests — only what package-level tests can't reach (signal handling,
CLI flag parsing edge cases, exit-code mapping).

## Coverage targets

These are the desired per-layer coverage targets. They are guidance for
reviews and local quality checks; not every threshold is hard-enforced in
CI today.

| Layer | Target | Failure action |
|---|---|---|
| `internal/domain/...` | ≥ 90% | fail PR |
| `internal/adapters/...` | ≥ 80% | fail PR |
| `internal/app/...` | ≥ 80% | fail PR |
| `internal/cli/...` | ≥ 60% (lots of urfave wiring) | warn |
| `cmd/reusable-ci/...` | ≥ 50% | warn |
| **Project total** | ≥ 80% | fail PR |

Run locally:

```text
just test-coverage          # merged unit + integration profile + HTML report
just test-coverage-html     # alias for test-coverage
go tool cover -func=bin/coverage.out
```

## Three rules

1. **Never mock at your own layer.** Domain tests don't use
   `fakeprovider`; adapter tests don't use `fakegitserver` *for the
   adapter under test*; app tests don't use mocked use cases. Use the
   real thing inside the layer; fake the layer below it.
2. **Every helper takes `t *testing.T` first** and registers
   `t.Cleanup` itself. No `defer cleanup()`, no global state, no `init()`.
3. **`testdata/` is a contract.** Don't generate fixtures at test time;
   commit them. Refresh with `-update`. Reviewer approves the diff.
