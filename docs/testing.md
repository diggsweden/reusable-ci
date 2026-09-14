# Testing conventions

Test architecture for the Go runtime. Follow these patterns when adding new
code; PR review checks that the layers are respected and the helpers are
used.

> Current testing policy for the Go codebase.

## Layers

Tests sit in one of the buckets below. Each has a different shape, build
tag, and CI gate. Two of them live outside this repository's `go test ./...`:
the black-box suite is its own repository, and the live tier needs a lab.

| Layer | What it tests | Build tag | Speed | Parallel | Network / CLI |
|---|---|---|---|---|---|
| **domain** | pure logic: parsers, transforms, decisions | (default) | <50ms | yes | no |
| **adapter** | I/O: real CLIs (`gpg`, `git`, `trivy`), real HTTP | `!short` for slow ones; `integration` for full-stack | <2s | yes (per package) | yes |
| **repo guards** | rules about the tree itself, not about behaviour | (default) | <1s | yes | no |
| **CLI smoke** | the binary as a black box, in this repo | `smoke` | <5s | no | builds binary |
| **black-box suite** | the binary against real toolchains and fixtures in the companion testsuite | `blackbox`, in that repo | minutes | yes | real tools; forge endpoints faked; package downloads may use network |
| **live / conformance** | the same scenario against configured real lab forges (currently GitLab and Forgejo, not GitHub) | `live` | minutes | no (`-p 1`) | a real lab |

Run them as:

```text
go test ./...                          # domain + fast adapter + repo guards
go test -tags=integration ./...        # + full-stack adapter
go test -tags=smoke ./cmd/...          # + CLI smoke tier
```

The provider-free tiers require `bash` and `jq` because `mockbinary` records
subprocess calls through a shell fixture. `jq` and the rest of the project
toolchain are pinned in `.mise.toml`; run `mise install` before the gates.

Architecture guards use bounded source analysis, not proof of executed behavior.
The environment-read guard uses the matching installed Go compiler's export
metadata and a populated module cache, with downloads disabled. It follows the
selected build configuration; it does not scan every platform/tag variant.
Its compiled working-directory check supplies the effective module cache before
replacing HOME and uses a fresh owned build cache. Exec-path analysis rejects
unsupported tracked paths but does not discover arbitrary command values carried
through fields, interfaces or captured closures.

The environment guard's model is bounded on purpose, and the boundary is not
where you might guess. It resolves stdlib object identity, string constants
across files and imports, and function values written once in the package. It
does not follow a lookup carried through a struct field, a reassigned variable
or a parameter — and it cannot be made to fail closed on those, which is the
part worth writing down. A rule that treated any `func(string) string` callee as
an environment read was built and reverted: it flags the sanctioned seam, where
a use case accepts an injected lookup from the composition root, and two
fixtures (`injected lookup`, `shadowed package parameter`) pin that pattern as
permitted. Nothing local separates a lookup handed in by the composition root
from one smuggled through a field. Distinguishing them needs interprocedural
analysis; until something does, "the guard reports the flows it models" is the
supported claim, not "no unmodelled flow exists".

Which files a guard is eligible to read follows from how it reads them, and the
two mechanisms disagree. The source-walking guards (layering, credential,
exec-wrap, provider-switch) parse with `go/parser`, which ignores build
constraints, so they read a `//go:build !linux` file on Linux. The environment
guard loads through export metadata, which honours them, so the same file is
invisible to it. Neither is wrong; neither announces itself. Every
build-constrained product file is therefore declared in
`TestGuardEligibility_ConstrainedAndGeneratedProductFilesAreDeclared`, and an
undeclared or changed one fails until someone says how the scanners treat it. A
variant the current build excludes must contain no direct environment read: the
guard that would judge it cannot see it, and the fix for a variant that needs
one is to run the environment guard under a matching GOOS, not to widen the
list. No product file in this repository is machine generated; a guard fails if
one appears, because a violation inside generated code is a report against the
generator rather than something a contributor can fix in place.

The live-parity source guard distinguishes recognized command-position
invocations, unresolved argv and explicit coverage debt. It follows internal
production imports conservatively, not per-verb runtime behavior. Missing
`version` live coverage and eight other group-specific debts remain visible;
neither inert JSON text nor a debt disposition is evidence of live parity.

### The black-box tier lives in another repository

`cmd/reusable-ci/smoke_test.go` is a smoke harness: it builds the binary and
checks the root contract (help, version, flag parsing, the exit-code ladder).
The bulk of the black-box scenarios (every ecosystem's real toolchain, the
signing round-trips, reproducibility, host isolation) are in the companion
repository, which drives the built binary against committed fixtures.

`docs/cli-black-box.md` is the catalogue for both. When adding a scenario,
the split is: if it needs a real toolchain or a fixture project, it belongs
in the testsuite repo; if it is about the CLI's own surface and needs nothing
installed, it can stay here.

Note the vocabulary: "e2e" is reserved for a real runner talking to a real
forge, which is neither of these. The in-repo tier is named for what it does,
`smoke`, so the word is free for the thing that earns it.

### The live tier

`internal/livetest/` holds a kit and `internal/livetest/conformance/` the
scenarios (`PAR-*`). One scenario body runs against every configured lab forge
that *claims* the capability it needs, so parity is enforced by shape rather
than by discipline. The alternative, a suite per forge, is how parity rots.

It exists because every other layer verifies an adapter against a fake written
from the same assumptions as the adapter, so an assumption that is wrong is
wrong in both places and both agree. Only a real forge breaks that tie. It is
what found GitLab's missing delete-then-create on re-release, the registry
adapter reporting permanent failures as retryable, and that Forgejo drops
untagged manifests while GitLab keeps them.

Run it against a disposable lab only:

```text
just test-live-full compose  # complete profile; repeat with k3s
```

#### Neutral-v2 live run

Run the full profile against each supported lab road from one fixed source tree:

1. Prove GitLab, Forgejo, their registries and runners, and the selected Fulcio
   trust endpoint are ready.
2. Mint a fresh absolute `LAB_TARGETS_FILE` with cleanup armed before the suite
   can contact a provider.
3. Set both owners, endpoint selectors, and
   `LAB_RUNNER_FORGES=gitlab,forgejo`.
4. Run `just test-live-full compose` or `just test-live-full k3s` once without
   `RC_LIVE_CONFIRM_DESTROY`; copy the exact confirmation from the refusal.
5. Export that confirmation and rerun. Require all selected provider scenarios
   to pass without skips, the source tree to remain unchanged, and credentials,
   target data, and cleanup runtime to be removed automatically.
6. On interruption, finish the advertised cleanup before minting another
   generation.

```text
^(TestRegistry_SyntheticArtifacts_RoundTripByDigest|TestInRunner_ForgeInjectedRegistryCredentialAuthenticates|TestLedger_RecordVerifyPromote_PreservesTheDigest|TestLedger_Cleanup_RemovesTheCandidateAndKeepsTheRelease|TestRollback_FromLedger_RemovesTheStagePointerOnly|TestRollback_FromJournal_(RemovesAReleaseTagThePromotionCreated|RestoresAMovingTagToItsPreviousImage)|TestSign_LedgerImages_ProducesAVerifiableSignature|TestInRunner_KeylessSigningAgainstTheLabCA)$
```

Run that explicitly partial diagnostic selection with
`just test-live-focused '<expression-above>'`. The `full` profile rejects a
filter, requires both GitLab and Forgejo owners and runners, verifies their
required capabilities and Fulcio mappings, and checks that both endpoints are
on the requested `compose` or `k3s` road.

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

When the log is not enough, because the job passed but produced the wrong artifact,
or the failure is in state teardown is about to delete, `RC_LIVE_KEEP_SCRATCH=1`
keeps the scratch repository so it can be inspected on the forge:

```text
RC_LIVE_KEEP_SCRATCH=1 go test -tags=live -run TestInRunner_... ./internal/livetest/...
```

It is an environment variable rather than a flag on purpose, awkward enough
that nobody leaves it on. Nothing accumulates either way: the next run deletes
the repository before recreating it, so what is kept is one generation.

It is **never in PR CI**: it is live, destructive, and human-invoked against a
disposable lab. `LAB_TARGETS_FILE` must name an owner-only neutral target
contract version 2; version 1 and every other version are rejected. The recipe
also requires an owner declared per forge (`RC_LIVE_<FORGE>_OWNER`) and an
explicit destroy confirmation naming the exact run, hosts, owners, and `rc-`
namespace.

The contract is JSON data, never sourced. It describes endpoint capabilities,
credentials, explicit OCI origins, an optional Fulcio origin with exact endpoint
issuer mappings, transport `ca_file`, and an object-form credential-cleanup
`command`/`contract_file` pair. Everything that authorises destruction is this
suite's own: the `rc-` namespace is compiled in, the owners are declared by the
operator, and the confirmation is typed against a run identity derived from all
three. A producer that supplied those too would hand out permission along with
the address, and any consumer holding the file could act on another consumer's
fixtures.

`LAB_RUNNER_FORGES` remains a separate operator-owned comma-separated list of
runners that are actually available. Endpoint `workflow-runs`, OCI, and Fulcio
facts do not imply runner availability. `workflow-runs` is required only by
scenarios that request `NeedsInRunner`; host-side scenarios can use an endpoint
without it. When a contract carries more than one endpoint of one kind, select
one exact endpoint name with `RC_LIVE_<FORGE>_ENDPOINT`.

The shell entrypoint first builds a small provider-free validator. That build is
outside cleanup ownership because no contract-supplied command is trusted yet;
if it fails, the entrypoint reports that the producer still owns the generation
and that cleanup must be inspected manually. The prebuilt helper then strictly
validates the entire contract, including exact-case and duplicate keys, UTF-8,
file safety, the 64 KiB limit, URL grammar, required capabilities, lifecycle
metadata, and the compiled disposable-host relationships.

Only after complete validation does the helper freeze the contract, a
certificate-only CA bundle, and the validated cleanup launcher bytes into
private run state. It records identity and digest facts for all three. The CA
source must be a bounded regular non-symlink owned by root or the current user,
with no group/world write access; its opened inode and pathname are rechecked
after the read. The original producer recovery file remains the launcher's exact
argument and is bound by canonical path, device, inode, mode, owner, size, and
SHA-256.

The shell pins the setup-built helper by an open descriptor, captures the frozen
launcher and original recovery path, and installs the trap before tool checks,
the product build, or provider work. At exit the pinned helper, not `realpath`,
`stat`, or `sha256sum`, descriptor-pins and verifies the frozen launcher,
revalidates the original recovery file, and executes the launcher through that
descriptor with the exact producer path. Replacing the source launcher cannot
change the frozen bytes; replacing the helper, frozen launcher, or recovery file
is refused and reported with the original pair for manual recovery. Malformed
JSON never supplies a command to the shell, and credential values are omitted
from generated diagnostics.

Every invocation also writes a durable evidence bundle outside cleanup-owned
temporary state. The default path is
`${XDG_STATE_HOME:-$HOME/.local/state}/reusable-ci/live-runs/<UTC-run-id>`;
set `RC_LIVE_EVIDENCE_ROOT` to use another dedicated private state location. The
entrypoint prints the exact path after cleanup. Each owner-only bundle contains
`live-run.log` after credential-pattern redaction, `source-files.sha256` for the
tracked and untracked working tree used by the build, `built-binaries.sha256`,
`metadata.txt`, and a verified `SHA256SUMS`. The log is limited to 10 MiB, the
source manifest to 2 MiB, and the other files to small fixed limits. Bundles
older than 30 days are removed when the next live run starts; copy a bundle to
longer-lived storage before then if it must be retained. The live tier is
human-invoked and has no owning workflow, so there is no CI artifact upload to
retrieve instead.

Authority classes are independent. Forge/API credentials may reach only the
selected endpoint roots. OCI credentials may reach only the declared registry
origin and the endpoint API origin explicitly needed for Forgejo/GitLab token
exchange. OIDC tokens may reach only the mapped Fulcio origin. Host-side clients
use the frozen CA, and each spawned product or cosign process is forced through a
per-invocation loopback CONNECT proxy for its one selected class. The keyless
runner receives a separately built static proxy, fetches its Forgejo token before
enabling that proxy, and then permits token-bearing traffic only to the exact
Fulcio authority. Approving Fulcio never makes it an OCI challenge or redirect
destination.

Any lab implementation may produce the contract. The canonical local fixture
corpus lives in `internal/livetest/testdata/`; its manifest records each local
path and SHA-256. Ordinary untagged tests verify those hashes, parse both
accepted contracts, exercise malformed variants, and test the shell
preflight/cleanup lifecycle without reaching a provider.

In this repository's own CI, the self-validation workflow runs:

- `just test` (unit + integration)
- `just test-smoke`
- `just test-fuzz` (seed corpus only; not active mutation fuzzing)

#### What the live tier actually covers

`providers.md` says what each forge *supports*. This says what has been
*observed* against a real one, a different axis, and the one that decides how
much a green suite is worth.

The full matrix covers GitLab and Forgejo on both supported roads:

| Group | What it settles |
|---|---|
| `TOK-1..4` | the run's own credential validates; a refusal is classified as a refusal and not as an outage; bot permissions match real access |
| `REL-1..5` | create with assets, verified through the raw forge API; asset upload as its own role; re-release of an existing tag; `release publish`; a 40 MiB asset served back with a matching digest |
| `REG-1..5` | registry fixture; image ledger against a real registry; cleanup deletes staging without disturbing the release; promotion rollback; `ResolveRegistryAuth` |
| `SIGN-1..2` | `container ledger sign` against a real registry; GitLab and Forgejo keyless OIDC against the lab's own Fulcio |
| `ART-1..3` | a forge without an artifact store refuses and says what is missing; one with a store does not refuse as though it lacked one; run-artifact round trip |
| `PKG-1..2` | `publish forge-packages` for npm and Maven, where the auth schemes diverge (Job-Token on GitLab, `Authorization: token` on Forgejo) and both are proven |
| `CAP-1..3` | the published matrix is rendered from the adapters and fails on drift; SARIF and provenance refusals |
| `RUN-1..5` | Forgejo resolves as Forgejo and not GitHub on a real runner; runtime self-report; annotation dialect; step summaries; output-file writes |
| `CHK-1` | `platform checkout` |
| `UX-1..4` | printed links resolve; `--dry-run` mutates nothing while the same verb does mutate when real; identical `--json` shape; identical exit code per failure class |

Current neutral-v2 migration acceptance passed on 2026-08-25 against Forge
Lab's Go-backed target generation. Compose and k3s both passed `REG-1..5` and
`SIGN-1..2` on GitLab and Forgejo, including declared OCI origins,
runner-injected registry authentication, exact Fulcio issuer mappings, and the
Fulcio-only CONNECT proxy. Both roads used the same sealed source-file manifest
digest, `2c939ede7b3b80023f1cfb15346bc265d789fc11101627841e8f7932c10bb00b`.
The Compose and k3s logs have SHA-256 digests
`144f6e9d991e68df67eee8f048afb7dee5358170be2320220f4032586f3ef5b1` and
`4e6abf9e737f5009d8651f04c0bc24221555891215d0a33ea73b035a70cca671`.
Scenario teardown passed and both generations removed their credentials,
recovery evidence, target files, and cleanup runtimes.

The detailed cross-repository rollout and defect record was archived outside
the active repository. Current acceptance evidence and contract and coverage
requirements remain fully documented here.

**The standing gap: GitHub is not in the lab.** There is no GitHub target, so
every scenario above is two-forge. GitHub's *exclusive* positive paths, SARIF
upload and the attestation API, are verified only against fakes written from
the same assumptions as the adapter, which is precisely the tie this tier exists
to break. It is the largest asymmetry in the suite and it does not close without
a real GitHub organisation.

One lesson worth keeping from the keyless work: signing **succeeded** and
verification **failed**, from a signing verb, three steps from the missing
anchor. Verification needs a trust root, because cosign checks the certificate
it was just issued and cannot learn a private CA's root on its own.

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
cmd/reusable-ci/smoke_test.go                 ← //go:build smoke
```

### Except the repo-wide guards

A guard that walks the whole tree is not a test of the package it happens to
sit in. It lives in one of the packages below, named for what it is
answerable for. This table is the list; the package docs do not repeat it.

`TestGuardPackagesAreDocumented` in `internal/syncguard` fails when a
`internal/*guard` package is missing a row here, so adding a package without
saying what it guards does not compile past CI.

| Package | Guards | Reads |
|---|---|---|
| `internal/archguard` | where code may live: import direction (ADR 0004), which layer may read the environment, mint a credential, or branch on the platform | Go source under `internal/` |
| `internal/lexiconguard` | a spelling, regex, or pattern literal is declared once | every file in the repo |
| `internal/syncguard` | a generated file still matches the Go it is generated from | `docs/`, `.reusable-ci/` |
| `internal/workflowguard` | the workflow contract adopters code against | `.github/workflows/`, `examples/` |

The rule: a guard that reads files outside its own package goes in one of
these; a test of `internal/cli` stays in `internal/cli`.

`internal/testutil/reporoot` resolves the repository root for all of them, so
none of them carries its own copy of that logic.

Two habits keep a guard useful:

- **Explain the fix, not the rule.** The failure message should say what to do
  next; the contributor can already see what they did.
- **Poison it once.** A guard that has never been seen to fail is a guard that
  may not work. Several carry a companion test that feeds them a violation on
  purpose (`TestJobLevelPlanGuardCatchesPoison`,
  `TestRuntimeImageTagGuardCatchesDivergence`).

No product-level `tests/` directory. The legacy Bats harness is retired;
Go behavior tests sit next to the code they exercise, and the remaining
shell-native bootstrap surface is tested under `scripts/...` with Go tests.

## Black-box by default, `_internal_test.go` for the rest

New test files use `package foo_test`, a black-box test from outside
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
for the loop variable. They are consistent enough across the codebase
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

Use `testify/require` (not `assert`): fail-fast on the first mismatch
gives a clear root-cause line instead of cascading errors. `t.Helper()`
on every test helper.

Failure output reads like spec lines: `--- FAIL: TestApply_TagRules/raw_value_emits_literal`.

This convention is partially adopted across the codebase; older tests use
ad-hoc shapes that are migrated opportunistically. New tests should follow the
convention from the start.

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
returns concrete types, and registers `t.Cleanup` itself, so tests never
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
| `fakegitserver` | In-memory transport (`Client()`) with route registration for GitHub API; no listener | HTTP fake server |
| `fakegitlabserver` | Same for GitLab API (preserves `%2F` in project paths) | HTTP fake server |
| `fixtures` | `//go:embed`'d sample data + named getters | embedded test fixture files |

When a use case needs a new helper concern, add a package. Don't grow
existing ones across responsibilities.

For env-sensitive tests, prefer `internal/testutil/testenv` directly or a
helper that composes it (`ghaenv`, `glabenv`, `isolatedgit`, `gpgkey`) so
HOME/XDG/git/GPG/proxy/locale isolation is applied consistently.

Tests that need a real working directory should prefer
`testfs.NewReal(t).Chdir()` over open-coded `os.Chdir` / `t.Chdir` blocks.
Because cwd is process-global, those tests must not call `t.Parallel()` at
that scope.

## Patterns by layer

### Domain: pure, parallel

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

### Adapter: real binaries and HTTP

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

### Application: stubbed deps

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

### CLI smoke: binary as black box

```go
//go:build smoke

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

These are the fewest tests: only what package-level tests can't reach (signal handling,
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

## Assertions that do not bite

A passing test is not evidence until you know what would make it fail. These
shapes recur, and each was found in this repository holding a real defect open.
When reviewing a test, check for them.

**A one-element fixture hides a loop.** All four image-ledger operations
(`Verify`, `Promote`, `Cleanup` and the promotion journal) were tested only with
`[]Entry{e}`. Truncating each loop to its first entry broke nothing, while a
release with several images would have gone half-verified, half-promoted and
half-cleaned. If the function takes a slice, one of its tests must pass at least
two elements, with the interesting one *not* first.

**Existence is not identity.** `sink.Single("version") != ""` passes on the
literal `"unknown"`, which is the value emitted when version resolution failed.
Assert the value.

**Inequality is not identity either.** Asserting two generated ids merely differ
accepts any scheme at all, including one that drops the shared prefix a consumer
groups by. Assert what each one is.

**Co-presence is not association.** Finding `"level": "error"` and
`"level": "note"` somewhere in a document does not show which finding got which:
swapping the mapping outright leaves both strings present. Read the field off
the object that owns it.

**A subset check suits prose, not data.** `strings.Contains` is fine on a log
line or a summary block. On argv, job outputs, published labels or a manifest it
cannot see an extra entry, a reordering, or two argv entries that should have
been one. Compare those whole.

**Message text is not the contract; the sentinel is.** Error strings may be
reworded freely. The wrapped `errs.*` value is what maps to an exit code, so
that is what a refusal test should assert. Where the refusal must also leave
nothing behind, assert that too: no output emitted, no file written, no tool
invoked.

**Poison the product to check the test.** Make the change the test claims to
catch and confirm it fails, then confirm the poison actually compiled and
applied first. A green run under a poison that never took is not evidence.

## Three rules

1. **Never mock at your own layer.** Domain tests don't use
   `fakeprovider`; adapter tests don't use `fakegitserver` *for the
   adapter under test*; app tests don't use mocked use cases. Use the
   real thing inside the layer; fake the layer below it.
2. **Every helper takes `t *testing.T` first** and registers
   `t.Cleanup` itself. No `defer cleanup()`, no global state, no `init()`.
3. **`testdata/` is a contract.** Don't generate fixtures at test time;
   commit them. Refresh with `-update`. Reviewer approves the diff.
