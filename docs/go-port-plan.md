<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Go Port — Plan and Architecture

A full-port plan for replacing the bash-based shared logic with a single Go
CLI binary while keeping workflow YAML, Containerfile, and templates as-is.
This document is the authoritative reference for the port.

> **Status:** in progress on `feat/go-port-phase-0`. The Go port lands
> **before** the GitLab Catalog adapter — the GitLab provider implementation
> lives in the Go binary as a typed adapter (Phase 3+), not as a parallel
> set of shell scripts. See [`gitlab.prep.md`](gitlab.prep.md) for the
> GitLab Catalog plan that consumes the resulting binary.

## Decisions

| Decision | Choice |
|---|---|
| Language | Go (latest stable; `go.mod` `go 1.24` at time of writing) |
| CLI framework | **urfave/cli v3** (latest major) |
| Module path | `github.com/diggsweden/reusable-ci` |
| Distribution | **Single binary**, baked into runtime image; macOS via downloaded archive |
| Public API | None. Everything `internal/` — no `pkg/` for v3 |
| Architecture inspiration | `forgejo/itiquette/gommitlint` — hexagonal / ports-and-adapters |
| Provider strategy | GitHub **and** GitLab adapters live in tandem; GitLab implemented as part of each port phase, not as a follow-up |
| Test framework | Go stdlib `testing` + `testify` (test-only dep) |

## Architecture

```text
                         ┌──────────────────────────┐
                         │  cmd/reusable-ci/main.go │  thin (~30 lines): build root, run
                         └──────────────┬───────────┘
                                        │ imports
                                        ▼
   ┌───────────────────────────────────────────────────────────────┐
   │  internal/cli/                  urfave/cli v3 wiring          │
   │  ├── commands/   (one or more *.go per subgroup)              │
   │  ├── deps/       provider + sink wiring                       │
   │  ├── docs/       CLI reference rendering                      │
   │  └── root/       root command assembly                        │
   └─────────────┬──────────────────────────────────────┬──────────┘
                 │                                      │
                 ▼                                      ▼
   ┌──────────────────────────┐         ┌──────────────────────────┐
   │  internal/app/           │         │  internal/adapters/       │
   │  Use cases — one per     │         │  I/O implementations.    │
   │  subcommand.             │         │  Each implements one or  │
   │                          │         │  more domain ports.      │
   │  Imports: domain, ports  │         │                          │
   │  Forbidden: adapters/*   │         │  Imports: domain         │
   │                          │         │  Forbidden: app, cli     │
   └─────────────┬────────────┘         └─────────────┬────────────┘
                 │                                    │
                 │ both depend on                     │
                 ▼                                    ▼
   ┌─────────────────────────────────────────────────────────────────┐
   │  internal/domain/        PURE — no I/O, no `os`, no `exec`     │
   │  ├── build/ container/ config/ errs/ git/ gpg/                │
   │  │   log/ output/ plan/ projecttype/ provider/ publish/       │
   │  │   release/ sbom/ security/ summary/ validate/ version/     │
   │  │     ↳ all data types + algorithms                          │
   │  └── PORTS (interfaces only):                                  │
   │      ├── provider/        Provider, EventContext, ReleaseSpec │
   │      ├── ci/              OutputSink, SummarySink, ManifestSink│
   │      └── git/             shared DTOs for git-facing boundaries│
   │                                                                │
   │  Imports: only stdlib + a small allow-list (yaml.v3, semver)   │
   │  Forbidden: everything else                                    │
   └─────────────────────────────────────────────────────────────────┘
```

### The dependency rule

Imports flow inward only:

- `domain/*` imports nothing outside stdlib + allow-list.
- `adapters/*` imports `domain/*`.
- `app/*` imports `domain/*` (never `adapters/*`).
- `cli/*` imports `app/*` and `domain/*`.
- `cli/deps` wires the provider and CI sink adapters.
- Some `cli/commands/*` packages also construct narrow tool adapters directly.

This rule is enforced by `golangci-lint`'s `depguard`. A breaking PR fails CI.

## Repo layout

```text
reusable-ci/
├── cmd/
│   ├── gen-cli-reference/                # prints docs/cli-reference.md to stdout
│   └── reusable-ci/
│       └── main.go                       # bootstraps the root command and process exit
├── internal/
│   ├── domain/                           # pure logic, NO I/O
│   │   ├── build/                        # pure build helpers and renderers
│   │   ├── ci/                           # PORTS: OutputSink, SummarySink, ManifestSink
│   │   ├── config/                       # artifacts.yml schema + derivation
│   │   ├── container/                    # image-name, namespace, OCI labels
│   │   ├── errs/                         # sentinel errors
│   │   ├── git/                          # shared git DTOs
│   │   ├── gpg/                          # pure GPG parsing helpers
│   │   ├── log/                          # slog helpers
│   │   ├── output/                       # annotation/output formatting
│   │   ├── plan/                         # release-policy decisions
│   │   ├── projecttype/                  # project-type enums/helpers
│   │   ├── provider/                     # PORT: Provider interface + value types
│   │   ├── publish/                      # publish-domain helpers
│   │   ├── release/                      # release spec, notes, changelog, policy
│   │   ├── sbom/                         # format decisions, zip naming
│   │   ├── security/                     # trivy/opengrep transforms, SARIF assembly
│   │   ├── summary/                      # stage-result composition, status icons
│   │   ├── validate/                     # tag/ref/changelog/token-scope rules
│   │   └── version/                      # bump, dev-version, tag invariants
│   ├── adapter/                          # I/O — implements domain ports
│   │   ├── github/                       # implements provider.Provider via gh + REST
│   │   ├── gitlab/                       # implements provider.Provider via glab + REST
│   │   ├── local/                        # for tests + dev loop
│   │   ├── git/                          # git CLI wrapper
│   │   ├── gpg/                          # gpg + agent + isolated GNUPGHOME
│   │   ├── ghaoutput/                    # implements ci.OutputSink for GHA heredoc
│   │   ├── stepsummary/                  # implements ci.SummarySink
│   │   ├── manifest/                     # implements ci.ManifestSink
│   │   ├── localfs/                      # OS-backed filesystem helper for release flows
│   │   ├── cargo/                        # cargo CLI wrapper
│   │   ├── gradle/                       # gradle CLI wrapper
│   │   ├── maven/                        # mvn CLI wrapper
│   │   ├── npm/                          # npm CLI wrapper
│   │   ├── syft/                         # syft CLI
│   │   ├── trivy/                        # trivy CLI
│   │   ├── opengrep/                     # opengrep CLI
│   │   └── xcode/                        # xcodebuild/security CLI wrappers
│   ├── app/                              # use cases — one file per subcommand or concern
│   │   ├── config/    container/    release/    version/
│   │   ├── security/  sbom/         summary/    validate/
│   │   └── plan/      publish/      build/
│   ├── cli/
│   │   ├── commands/                     # subgroup and command wiring
│   │   ├── deps/                         # provider + sink wiring helpers
│   │   ├── root.go                       # builds *cli.Command tree
│   │   ├── render.go                     # CLI reference rendering
│   │   └── *_test.go                     # CLI package tests
│   ├── platform/
│   │   └── env.go                        # CI_PLATFORM detection
│   └── testutil/                         # replaces tests/test_helper.bash
│       ├── golden/    isolatedgit/   mockbinary/   gpgkey/
│       ├── ghaenv/    glabenv/       fakeprovider/  fakeoutputsink/
│       ├── fakegitserver/    fakegitlabserver/      fixtures/
│       └── testdata/
├── templates/                            # //go:embed at build time
│   └── gitcliff/{default,keepachangelog}.toml
├── containers/runtime/Containerfile      # add Go-builder stage; copy binary in
├── .github/workflows/                    # YAML; edited per Phase 11
├── .gitlab/ci/                           # GitLab adapter, grown per phase
├── examples/                             # unchanged
├── docs/
│   ├── go-port-plan.md                   # this file
│   ├── cli-reference.md                  # auto-generated from urfave help (Phase 12)
│   ├── architecture.md                   # diagrams + port catalogue
│   └── …
├── go.mod
├── go.sum
├── .goreleaser.yml
└── justfile
```

## Library — package responsibilities

### `internal/domain/` (pure, the "what")

| Package | Responsibility | Key exports |
|---|---|---|
| `domain/build` | pure build helpers and summary renderers | `IsSnapshot`, `RenderMavenSummary`, `ParseXcodeVersionFromPbxproj` |
| `domain/ci` *(PORT)* | sinks the use cases write into | `OutputSink`, `SummarySink`, `ManifestSink` |
| `domain/config` | `artifacts.yml` schema + parse + validate + derived fields | `Config`, `Parse`, `Validate`, `Derive` |
| `domain/container` | image-name resolution, namespace policy, OCI labels | `ResolveImageName`, `ValidateNamespace`, `AssembleLabels` |
| `domain/errs` | sentinel errors and exit codes | `ErrUnsupportedPlatform`, `ErrInvalidConfig`, `ExitCodeFromError` |
| `domain/git` | shared DTOs for git-facing boundaries | `CommitInput`, `CommitInfo` |
| `domain/gpg` | pure parsing/helpers for GPG metadata | `DecodeKey`, `ParseColonsOutput`, `ParseKeygrips` |
| `domain/log` | logging helpers | `LevelTrace`, `ReplaceLevelAttr` |
| `domain/output` | annotation and output formatting | `Annotator`, `Parse`, `ParseAndResolve` |
| `domain/plan` | release-policy decisions | `Resolve`, plan interfaces |
| `domain/projecttype` | project-type enum/helpers | `Type`, `Parse`, known constants |
| `domain/provider` *(PORT)* | adapter contract + value types | `Provider`, `Platform`, `EventContext`, `RepoMetadata`, `ReleaseSpec` |
| `domain/publish` | publish-domain helpers | App Store / Google Play auth and metadata helpers |
| `domain/release` | release notes, asset policy, checksums/SBOM naming | `CollectAssets`, `IsReleaseArtifact`, constants |
| `domain/sbom` | canonical SBOM naming and manifest parsing helpers | `ZipName`, manifest parsers |
| `domain/security` | trivy JSON → GitLab transforms; SARIF helpers | `TrivyToGitLabDep`, `TrivyToGitLabContainer`, `EnrichGitHubSARIF` |
| `domain/summary` | stage-result composition; status normalisation | `StageResultEnvelope`, `NormalizeResult`, `AggregateResults` |
| `domain/validate` | tag format, signature, ref-type, token scopes, changelog | pure validation helpers |
| `domain/version` | SemVer bump rules, dev-version, tag invariants | `Bump`, `GenerateDev`, `StripVPrefix` |

### `internal/adapters/` (I/O, the "how")

| Package | Implements | External tool / API |
|---|---|---|
| `adapter/github` | `provider.Provider` | `gh` CLI + `api.github.com` REST |
| `adapter/gitlab` | `provider.Provider` | `glab` CLI + `gitlab.com/api/v4` REST |
| `adapter/local` | `provider.Provider` | env-only (tests + dev loop) |
| `adapter/git` | direct call from app | `git` CLI |
| `adapter/gpg` | direct call from app (crypto-sensitive ops) | `gpg`, `gpg-connect-agent`, isolated `GNUPGHOME` |
| `adapter/ghaoutput` | `ci.OutputSink` | `$GITHUB_OUTPUT` heredoc |
| `adapter/stepsummary` | `ci.SummarySink` | `$GITHUB_STEP_SUMMARY` / `$CI_SUMMARY_FILE` |
| `adapter/manifest` | `ci.ManifestSink` | `.ci-results/<stage>-result.json` |
| `adapter/localfs` | release-flow filesystem queries | OS filesystem |
| `adapter/{cargo,gradle,maven,npm,syft,trivy,opengrep,xcode}` | direct call | respective CLIs |

Adapters are kept narrow and focused on one tool or sink each.

### `internal/app/` (use cases)

One subdirectory per CLI subgroup. Files are named after one subcommand
or one closely related concern. Each exported entry point accepts only the
narrow dependencies it needs (provider, sink, or tool interface), and the
package keeps the orchestration logic that would otherwise live in shell
scripts.

Use cases stay short and mostly coordinate domain helpers plus I/O.

### `internal/cli/` (urfave/cli v3 binding)

| Package | Responsibility |
|---|---|
| `cli/` | builds the root `*cli.Command` tree, wires global flags + `Before` hook for log setup, and renders the generated CLI reference |
| `cli/commands/` | subgroup and command wiring; some commands also construct narrow tool adapters |
| `cli/deps/` | provider and CI sink wiring. `Build(ctx) (*Deps, error)` reads platform via `internal/platform` and returns typed `*Deps`. |

### `internal/platform/`

One file. `Detect() provider.Platform` reads `GITHUB_ACTIONS` / `GITLAB_CI`
/ falls through to `local`. No types, no logic — just env detection.

### `internal/testutil/`

Test-only helpers (built in Phase 0). Already enumerated above. None of
these import `adapters/*` directly — they're pure or implement domain
ports as test doubles.

## Port definitions

These are the only interfaces that cross the domain↔adapter boundary.
Everything else is concrete.

### `domain/provider.Provider`

```go
package provider

import "context"

type Provider interface {
    Name() Platform

    // Phase 3
    ResolveContext(ctx context.Context) (*EventContext, error)

    // Phase 4
    FetchRepoMetadata(ctx context.Context, repo string) (*RepoMetadata, error)

    // Phase 5
    ValidateToken(ctx context.Context, token string, scopes []string) error
    ValidateBotPermissions(ctx context.Context, repo string) error

    // Phase 7
    CreateRelease(ctx context.Context, spec ReleaseSpec) (*Release, error)
}

type Platform string

const (
    PlatformGitHub Platform = "github"
    PlatformGitLab Platform = "gitlab"
    PlatformLocal  Platform = "local"
)

type EventContext struct {
    RefName   string
    RefType   RefType        // Branch, Tag, PR
    SHA       string
    ShortSHA  string
    Branch    string
    PRNumber  string
    EventName string
    Repo      string         // "owner/repo" or "group/project/path"
    RepoURL   string
}
```

Methods are added per phase, **additively only**. Each adapter
(`github`, `gitlab`, `local`) implements every method that exists at the
current phase.

### `domain/ci`

```go
package ci

import "context"

type OutputSink interface {
    Set(ctx context.Context, key, value string) error
    SetMultiline(ctx context.Context, key string, lines []string) error
    Close(ctx context.Context) error
}

type SummarySink interface {
    Append(ctx context.Context, markdown string) error
}

type ManifestSink interface {
    Write(ctx context.Context, stage string, result map[string]any) error
}
```

`adapter/ghaoutput` is the current `OutputSink` implementation. It writes
GitHub-style heredoc output and can fall back to `CI_OUTPUT` for local/dev
execution.

## Composition root — `cli/deps`

```go
// internal/cli/deps/deps.go
package deps

type Deps struct {
    Platform     provider.Platform
    Provider     provider.Provider
    OutputSink   ci.OutputSink
    SummarySink  ci.SummarySink
    ManifestSink ci.ManifestSink
}

func Build(ctx context.Context) (*Deps, error) {
    p := platform.Detect()
    d := &Deps{Platform: p}

    switch p {
    case provider.PlatformGitHub:
        d.Provider = github.New()
    case provider.PlatformGitLab:
        d.Provider = gitlab.New()
    case provider.PlatformLocal:
        d.Provider = local.New()
    default:
        return nil, fmt.Errorf("unsupported platform: %s", p)
    }

    d.OutputSink = ghaoutput.NewFromEnv()
    d.SummarySink = stepsummary.NewFromEnv()
    d.ManifestSink = manifest.NewFromEnv()
    return d, nil
}
```

This is the **only place** that knows how to translate "we're running
on GitHub vs GitLab vs local" into the provider and sink set. Narrow tool
adapters such as `adapter/git`, `adapter/gpg`, or `adapter/maven` are still
constructed directly by some command bindings.

## CLI binding — urfave/cli v3

`cmd/reusable-ci/main.go`:

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/diggsweden/reusable-ci/internal/cli"
)

var (
    version = "dev"
    commit  = ""
    date    = ""
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    cmd := cli.New(cli.BuildInfo{Version: version, Commit: commit, Date: date})
    if err := cmd.Run(ctx, os.Args); err != nil {
        slog.Error("command failed", "err", err)
        os.Exit(1)
    }
}
```

Subcommand binding (one per file in `cli/commands/`):

```go
// internal/cli/commands/container/container.go
func New() *cli.Command {
    return &cli.Command{
        Name:  "container",
        Usage: "container-image helpers",
        Commands: []*cli.Command{
            metadataCmd(),
            resolveNameCmd(),
            validateNamespaceCmd(),
            extractBinariesCmd(),
        },
    }
}

func metadataCmd() *cli.Command {
    return &cli.Command{
        Name:  "metadata",
        Usage: "compute image tags and OCI labels from artifacts.yml-style rules",
        Flags: []cli.Flag{
            &cli.StringFlag{Name: "image-name", Required: true,
                Sources: cli.EnvVars("IMAGE_NAME")},
            &cli.StringFlag{Name: "tag-rules", Sources: cli.EnvVars("TAG_RULES")},
            &cli.StringFlag{Name: "flavor",    Sources: cli.EnvVars("FLAVOR")},
            &cli.BoolFlag{Name: "emit-labels", Sources: cli.EnvVars("EMIT_LABELS")},
            flags.OutputSink(),
        },
        Action: func(ctx context.Context, cmd *cli.Command) error {
            d, err := deps.Build(ctx)
            if err != nil { return fmt.Errorf("init deps: %w", err) }
            return container.Metadata(ctx, d, container.MetadataInput{
                ImageName:      cmd.String("image-name"),
                TagRules:       cmd.String("tag-rules"),
                Flavor:         cmd.String("flavor"),
                EmitLabels:     cmd.Bool("emit-labels"),
                OciDescription: cmd.String("oci-description"),
                OciLicense:     cmd.String("oci-license"),
            })
        },
    }
}
```

## CLI surface (~50 subcommands, mirrors current script verbs)

```text
reusable-ci config  parse | validate | dev-context

reusable-ci release create | gpg-import | gpg-cleanup | sign | sbom | notes | checksums | sbom-zip
reusable-ci version bump | commit-push | move-tag | generate-dev
reusable-ci container metadata | resolve-name | validate-namespace | extract-binaries
reusable-ci security trivy-scan | trivy-to-gitlab-dep | trivy-to-gitlab-container | opengrep | upload-sarif
reusable-ci sbom    generate | transform
reusable-ci summary build-stage | publish-stage | prereq | quality-check
reusable-ci validate token | permissions | ref-type | tag-format | tag-signature | tag-uniqueness | tag-commit | changelog
reusable-ci plan    write-release-interface | write-dev-release-interface | get-file-pattern
reusable-ci publish maven-central | npm | apple | google
reusable-ci build   maven | npm | gradle | gradle-android | xcode-ios
```

Each subcommand reads env vars **and** accepts equivalent flags, so
it's testable without env mutation.

## Test migration plan (gommitlint-inspired)

### Test architectural principles

1. **Tests live next to source.** `domain/tagrule/apply.go` ↔ `domain/tagrule/apply_test.go`. No `tests/` directory at the project root.
2. **Table-driven everything.** Each test is a slice of `struct{name, input, expected, wantErr}`; loop with `t.Run(tc.name, ...)`.
3. **Three layers, three styles**:
   - Domain tests — pure, fast, parallel, no I/O. Default build tag.
   - Adapter tests — real external binaries / FS / network when allowed. Skipped under `-short`. Some need `//go:build integration`.
   - CLI smoke tests — build the binary in `TestMain`, exec it. `//go:build e2e`.
4. **Golden files** for non-trivial outputs. `testdata/golden/<name>.json`. `-update` flag refreshes locally.
5. **`testdata/`** for fixtures: `artifacts.yml` samples, GPG colons output, sample changelogs.
6. **Helpers in `internal/testutil/`** — one package per concern; each registers `t.Cleanup` itself.
7. **Property-based + fuzz tests** on every text parser.
8. **No `testify` outside test files.** Production code uses stdlib `errors` + `slog`.

### `internal/testutil/` packages

| Package | Replaces | Purpose |
|---|---|---|
| `golden/` | bats `assert_output --partial` | golden-file equality with `-update` flag |
| `isolatedgit/` | `common_setup_with_isolated_git` | temp repo with fixtures; cleanup via `t.Cleanup` |
| `mockbinary/` | `create_mock_binary` | shell stubs on `PATH`, recording argv/stdin/env per invocation |
| `gpgkey/` | per-test setup blocks | ephemeral `GNUPGHOME`, throwaway key, killagent on cleanup |
| `ghaenv/` | `common_setup_with_github_env` | tempfile-backed `GITHUB_OUTPUT` / `GITHUB_STEP_SUMMARY` |
| `glabenv/` | (new — no bats equivalent) | GitLab dotenv-style sink + `CI_*` env |
| `fakeprovider/` | (new) | in-memory `provider.Provider` with configurable returns + call recorder |
| `fakeoutputsink/` | (new) | in-memory `ci.OutputSink` capturing to map |
| `fakegitserver/`, `fakegitlabserver/` | (new) | `httptest.NewServer` with canonical routes for each REST API |
| `fixtures/` | bats fixture files | `//go:embed testdata/fixtures/*` |

### Test patterns

**Domain test:**

```go
func TestApply(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name  string
        rule  tagrule.Rule
        evt   provider.EventContext
        want  string
        fired bool
    }{
        {name: "raw value emits literal", rule: ..., want: "main", fired: true},
        {name: "ref/branch on tag does not fire", rule: ..., fired: false},
        // ~25 cases mirror current bats coverage
    }
    for _, tc := range tests {
        tc := tc
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            got, fired, err := tagrule.Apply(tc.rule, &tc.evt)
            require.NoError(t, err)
            require.Equal(t, tc.fired, fired)
            if tc.fired { require.Equal(t, tc.want, got) }
        })
    }
}
```

**Property + fuzz on parsers:**

```go
func TestParse_NeverPanics(t *testing.T) {
    quick.Check(func(s string) bool {
        _, _ = tagrule.Parse(s)
        return true
    }, &quick.Config{MaxCount: 5000})
}

func FuzzParse(f *testing.F) {
    f.Add("type=raw,value=main,enable=true")
    f.Fuzz(func(t *testing.T, s string) { _, _ = tagrule.Parse(s) })
}
```

**Adapter test with mocked CLI:**

```go
func TestRepo_FetchMetadata(t *testing.T) {
    bin := mockbinary.New(t)
    bin.Add("gh", `if [ "$1" = "api" ] && [ "$2" = "repos/owner/repo" ]; then
        cat <<JSON
{"description":"x","html_url":"https://github.com/owner/repo","license":{"spdx_id":"Apache-2.0"}}
JSON
    fi`)

    p := github.New(github.WithGHPath(bin.Path("gh")))
    md, err := p.FetchRepoMetadata(t.Context(), "owner/repo")
    require.NoError(t, err)
    require.Equal(t, "Apache-2.0", md.LicenseSPDX)
    require.Len(t, bin.Invocations("gh"), 1)
}
```

**App-layer test with stubbed deps:**

```go
func TestMetadata(t *testing.T) {
    fp := fakeprovider.New(t).
        WithEventContext(provider.EventContext{
            RefType: provider.RefTypeTag, RefName: "v1.2.3",
        }).
        WithRepoMetadata(provider.RepoMetadata{LicenseSPDX: "MIT"})
    fs := fakeoutputsink.New(t)

    err := container.Metadata(t.Context(), &deps.Deps{Provider: fp, OutputSink: fs},
        container.MetadataInput{
            ImageName:  "ghcr.io/x/y",
            TagRules:   "type=semver,pattern={{version}},enable=true",
            EmitLabels: true,
        })
    require.NoError(t, err)
    require.Equal(t, []string{"ghcr.io/x/y:1.2.3"}, fs.Multiline("tags"))
    require.Contains(t, fs.Multiline("labels"),
        "org.opencontainers.image.licenses=MIT")
}
```

### Migration status

The Bats migration is complete. Logic-backed shell coverage moved into Go
tests next to `internal/domain/`, `internal/adapters/`, `internal/app/`,
`internal/cli/`, and the remaining shell runtime surface under `scripts/`.

### Coverage gates (CI)

| Layer | Target | Failure action |
|---|---|---|
| `internal/domain/...` | ≥ 90% | fail PR |
| `internal/adapters/...` | ≥ 80% | fail PR |
| `internal/app/...` | ≥ 80% | fail PR |
| `internal/cli/...` | ≥ 60% | warn |
| `cmd/reusable-ci/...` | ≥ 50% | warn |
| **Project total** | ≥ 80% | fail PR |

## Migration phases — each ends cross-provider

| Phase | Scope | Provider methods | gh adapter | glab adapter | Bats retired |
|---|---|---|---|---|---|
| **0** | Scaffold: `go mod`, urfave/cli root, `deps` factory, `platform.Detect()`, sentinel "not implemented" subcommands. **Build all `internal/testutil/*` packages** including their own tests. | none | n/a | n/a | none |
| **1** | **Pure-domain ports**: `config parse`, `config validate`, `container resolve-name`, `container validate-namespace`, `sbom transform`, `security trivy-to-gitlab-{dep,container}`, `summary normalize-result` | none | n/a | n/a | `parse-artifacts-config`, `resolve-image-name`, `validate-namespace`, `trivy-to-gitlab-*`, `sbom transform-*` |
| **2** | **Local-tool adapters**: `release gpg-import`/`gpg-cleanup`, `version commit-push`/`move-tag`/`bump`/`generate-dev`, `release sign`, `sbom-zip`, `release checksums` (uses `gpg`/`git`/`shasum` only) | none | n/a | n/a | `release/{gpg-*,sign-release-artifacts,generate-checksums,create-sbom-zip}`, `version/{bump,commit-and-push,move-tag,generate-dev-version}` |
| **3** | **Provider interface born**: `ResolveContext` only. github + gitlab + local adapters. No user-visible feature yet. | `+ResolveContext` | reads `GITHUB_*` env | reads `CI_COMMIT_*` env | none |
| **4** | `container metadata`. **First cross-provider feature shipped end-to-end.** | `+FetchRepoMetadata` | `gh api repos/...` | `glab api projects/...` | `compute-image-metadata` (29) |
| **5** | `validate token`, `validate permissions`, `validate ref-type`, `validate tag-{format,signature,uniqueness,commit}`, `validate changelog` | `+ValidateToken`, `+ValidateBotPermissions` | `gh api`, `curl api.github.com` | `glab api`, `curl gitlab.com/api/v4` | `tests/validate/*` (~120) |
| **6** | `plan write-release-interface`, `plan write-dev-release-interface`, `plan get-file-pattern`. Provider-influenced policies via `Name()`. | none new | unchanged | unchanged | `tests/plan/*` |
| **7** | `release create`, `release notes`, `release sbom`. **The phase where gh and glab paths diverge most.** Per-asset upload semantics differ; abstract behind `ReleaseSpec`. | `+CreateRelease` | `gh release create` + `gh release upload` | `glab release create` + `glab release upload` | `tests/release/*` (`create-release`, `prepare-release-notes`, `resolve-*`, `validate-changelog`) |
| **8** | `summary build-stage`, `summary publish-stage`, `summary prereq`, `summary quality-check`. Mostly provider-agnostic (writes to `OutputSink` + `SummarySink`). | none | unchanged | unchanged | `tests/summary/*` (~80) |
| **9** | `build maven`, `build npm`, `build gradle`, `build gradle-android`, `build xcode-ios`. Toolchain wrappers. | reuse | reuse | reuse | `tests/build/*` |
| **10** | `publish maven-central`, `publish npm`, `publish apple`, `publish google`. External-registry calls. | reuse | reuse | reuse | `tests/publish/*` |
| **11** | **Workflow YAML migration**: replace `bash /opt/.../scripts/foo/bar.sh` with `reusable-ci foo bar`. **Build out `.gitlab/ci/` adapter YAML** mirroring each operation. In-repo `.gitlab-ci.yml` self-test exercises real GitLab. | reuse | unchanged | unchanged | none yet |
| **12** | **Decommission `scripts/` + bats**. Replace `docs/scripts.md` with auto-generated `docs/cli-reference.md`. | n/a | n/a | n/a | **all remaining ~1108 retired** |

End state after Phase 12: GitHub *and* GitLab adapters live, tested,
and producing identical artefacts (where each platform supports them)
for every workflow operation. No legacy bash. The runtime image
carries the binary as `/usr/local/bin/reusable-ci`.

## Distribution

`.goreleaser.yml`:

```yaml
project_name: reusable-ci
builds:
  - main: ./cmd/reusable-ci
    env: [CGO_ENABLED=0]
    goos:    [linux, darwin]
    goarch:  [amd64, arm64]
    ldflags: ['-s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}']
archives:
  - format: tar.gz
    files: [LICENSE, README.md]
checksum: { name_template: 'checksums.txt' }
sboms:    [{ artifacts: archive }]
signs:    [{ cmd: cosign, signature: '${artifact}.sig', certificate: '${artifact}.pem' }]
release:
  github: { owner: diggsweden, name: reusable-ci }
```

Containerfile gets a Go-builder stage:

```dockerfile
FROM golang:1.24-alpine AS gobuilder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY templates/ templates/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/reusable-ci ./cmd/reusable-ci

FROM debian:13-slim AS base
# ... existing apt + install-* helpers ...
COPY --from=gobuilder /out/reusable-ci /usr/local/bin/reusable-ci
RUN reusable-ci --version
```

macOS workflows (`build-xcode-ios`, `publish-apple-appstore`) currently
install the binary via `go install` at the requested ref.

## Allowed import edges (the depguard rule)

```yaml
# .golangci.yml
linters:
  enable: [depguard]

linters-settings:
  depguard:
    rules:
      domain:
        list-mode: lax
        files: ["**/internal/domain/**"]
        allow:
          - $gostd
          - gopkg.in/yaml.v3
          - github.com/Masterminds/semver/v3
        deny:
          - pkg: github.com/diggsweden/reusable-ci/internal/adapter
            desc: "domain must not import adapter"
          - pkg: github.com/diggsweden/reusable-ci/internal/app
            desc: "domain must not import app"
          - pkg: github.com/diggsweden/reusable-ci/internal/cli
            desc: "domain must not import cli"
          - pkg: os/exec
            desc: "domain must be pure (no subprocess)"
          - pkg: net/http
            desc: "domain must be pure (no network)"

      app:
        files: ["**/internal/app/**"]
        deny:
          - pkg: github.com/diggsweden/reusable-ci/internal/adapter
            desc: "app must use domain ports, not adapters"

      adapter:
        files: ["**/internal/adapters/**"]
        deny:
          - pkg: github.com/diggsweden/reusable-ci/internal/app
            desc: "adapters must not call app"
          - pkg: github.com/diggsweden/reusable-ci/internal/cli
            desc: "adapters must not depend on cli"
```

This is the **single most important** safeguard. The architecture only
stays clean if the rule is enforced mechanically.

## Tooling and CI gates

`golangci-lint` config:

- `errcheck` — every error checked
- `gosec` — security smells; explicitly allowlist subprocess uses in adapters
- `revive` — exported names with godoc, no naked returns
- `gocritic` — performance / style
- `unparam` — unused parameters
- `forbidigo` — bans `panic`, `os.Exit`, `log.Fatal` outside `main`; bans `fmt.Print*` in adapters (use `slog`)
- `depguard` — see above

`justfile` becomes mostly delegations:

```text
build:
    go build -ldflags "-X main.version=$(git describe) -X main.commit=$(git rev-parse --short HEAD)" -o bin/reusable-ci ./cmd/reusable-ci

test:
    go test -race -timeout 120s ./...

lint:
    golangci-lint run
    actionlint .github/workflows/*.yml
    yamllint .

generate:
    go run ./cmd/reusable-ci doc > docs/cli-reference.md
```

CI (extends `self-pullrequest.yml`):

```yaml
- name: Go unit + race
  run: go test -race -timeout 120s -coverprofile=coverage.out ./...
- name: Coverage gate
  run: |
    pct=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}' | tr -d '%')
    awk -v p="$pct" 'BEGIN { exit !(p+0 >= 80) }'
- name: Go integration tests
  run: go test -tags=integration -timeout 300s ./internal/adapters/...
- name: Go e2e (binary smoke)
  run: |
    go build -o bin/reusable-ci ./cmd/reusable-ci
    PATH="$PWD/bin:$PATH" go test -tags=e2e -timeout 300s ./cmd/...
- name: Bats — bash parity (until phase 12)
  run: just test
```

## Estimated scope

| Phase | Implementation | Test-writing | Bats-retirement | Total |
|---|---:|---:|---:|---:|
| 0 | 2 | 2 | 0 | 4 |
| 1 | 3 | 3 | 0 | 6 |
| 2 | 4 | 3 | 0 | 7 |
| 3 | 2 | 1 | 0 | 3 |
| 4 | 2 | 2 | 1 | 5 |
| 5 | 3 | 3 | 2 | 8 |
| 6 | 2 | 2 | 1 | 5 |
| 7 | 5 | 4 | 2 | 11 |
| 8 | 3 | 2 | 2 | 7 |
| 9 | 4 | 3 | 1 | 8 |
| 10 | 4 | 3 | 1 | 8 |
| 11 | 4 | 2 | 0 | 6 |
| 12 | 1 | 0 | 1 | 2 |
| **Sum** | **39** | **30** | **11** | **80** |

≈ **80 engineering days** for one developer. ≈ 40% test work — disciplined
rather than bloated, due to the gommitlint-derived patterns. Wall-clock
compresses to 2–3 months with parallelisation across phases that don't
share files.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| 1108 bats → Go tests is the biggest cost | Don't translate all at once. Bats keep running against the binary through Phase 11. Convert table-driven bats to Go table tests last; keep gnarly fixture bats (gpg, isolated_git) until adapter tests are mature. |
| Subprocess shelling out adds the same dependency surface as bash | Wrapped in typed Go: `tool.GH().Release(ctx, ReleaseSpec{...})`. Single point of failure for each external CLI; easy to mock via `mockbinary`. |
| urfave/cli v3 community is smaller than Cobra | Cobra is bigger but uses more reflection and has more surface. urfave v3 is simpler, has explicit `context.Context` support, env-var binding via `cli.EnvVars`, and the API maps almost 1:1 to gommitlint's pattern. v3 has been stable for over a year. |
| Composition root (`deps.Build`) becomes a god factory | Keep it dumb — switch on platform, return `*Deps`. No DI containers. |
| Breaking workflow consumers | Pin runtime image tag (`runtime-base:v3.0.0`). Consumers pinned to v2 keep working. Major-version bump signals the change. |
| Templates loaded from disk vs embedded | Use `//go:embed`. Drop `/opt/reusable-ci/templates/` baking step from the Containerfile in Phase 6. |
| `gpg --batch` colons-format parsing edge cases | Domain-only parser with thorough table-driven tests + property tests via `testing/quick`. |
| Cross-provider test parity drift | Phase 11's in-repo `.gitlab-ci.yml` self-test runs against real GitLab; quirks discovered there feed back as adapter fixes. |
| Coverage gate becomes blocker noise | Only enforce ≥ 80% on `domain/`/`adapter/`/`app/` initially. Tighten to whole-repo after Phase 12. |
| Real-`gpg` adapter tests flake on different gpg versions | Pin gpg version in CI image (already true: runtime container has gpg 2.4.4). Adapter tests skip if `gpg --version` < 2.4. |
| Real-`git` tests pollute developer's global gitconfig | `isolatedgit.NewRepo` sets `HOME=t.TempDir()` and writes its own `~/.gitconfig` via `GIT_CONFIG_GLOBAL`. |

## What stays in bash forever

- `containers/runtime/Containerfile` — declarative Dockerfile syntax
- `templates/gitcliff/*.toml` — declarative config
- `.github/workflows/*.yml` — GHA YAML
- `.gitlab/ci/*.yml` — GitLab YAML (Phase 11)
- `justfile` — task runner

## What we're NOT doing

- No Go library (`pkg/`) — single-binary distribution only.
- No Cobra. urfave/cli v3.
- No DI container. The composition root is a switch statement.
- No custom-rolled implementations where a stable, mature Go library exists.
  See "Library policy" below.
- No removal of GHA YAML, Containerfile, or templates — those declarative
  artefacts stay in their idiomatic forms.

## Library policy

For any operation, the order of preference is:

1. **Use a stable, mature Go library** if one exists with an API broad enough
   to cover what we need. Libraries give us typed values, real error types,
   structured logging, and unit-testable mocks.
2. **Shell out to a stable CLI** if no good library exists, or if the
   library's transitive dependency closure is disproportionate to what we
   use. The CLI's flag surface is the contract.
3. **Custom advanced implementations** are the last resort — only when
   neither a library nor a CLI fits.

Specific bindings as of this plan:

| Tool / API | Choice | Library | Notes |
|---|---|---|---|
| GitHub REST API | **library** | `github.com/google/go-github/v60` | typed, official, tracks API; used by `adapter/github/` instead of shelling out to `gh api` |
| GitLab REST API | **library** | `gitlab.com/gitlab-org/api/client-go` | mature; replaces `glab api` for the GitLab provider adapter |
| OAuth / token validation | **library** | `golang.org/x/oauth2` (transitively via go-github / go-gitlab) | |
| YAML parsing | **library** | `gopkg.in/yaml.v3` | already in domain allow-list |
| SemVer parsing | **library** | `github.com/Masterminds/semver/v3` | already in domain allow-list |
| OCI / container metadata | **library** | `github.com/regclient/regclient` (TBD) | for any registry interactions beyond what `docker buildx imagetools` covers |
| SBOM generation (CycloneDX, SPDX) | **library, library, or CLI** | evaluate `github.com/anchore/syft` library mode | if the dep closure is ≤ ~50 MB, use the library; else shell out to syft CLI. Decision in Phase 8 when SBOM/* lands. |
| Vulnerability scanning (Trivy) | **CLI** | (none viable) | Trivy library mode pulls in the entire vulnerability DB plumbing. CLI surface is stable; shell out. |
| OpenGrep | **CLI** | (no Go library) | written in OCaml; no library option. Shell out. |
| GPG | **CLI** | (no library) | gnupg's full feature set (agent, keyring, signing) requires the CLI; no Go binding covers it. Shell out. |
| Git | **CLI** | (libraries too narrow) | `go-git` lacks features we use (real signing, push semantics, submodules). Shell out — `git` is universally available. |
| Docker / Buildx | **CLI** | (none viable for our use) | `docker buildx imagetools create` semantics not exposed by any library. Shell out. |
| Cosign / SLSA attestations | **library** if available | evaluate `github.com/sigstore/cosign/v2` Go API | otherwise shell out to `cosign`. |
| Logging | **library (stdlib)** | `log/slog` | no third-party logging lib. |
| CLI framework | **library** | `github.com/urfave/cli/v3` | per the decisions table. |

The "shell out" cases above stay behind dedicated adapter packages so each
external CLI has one Go-typed call surface. Tests use `mockbinary` for those.

For each new external concern that arrives in a later phase, the same
question gets asked: is there a stable mature Go library? If yes, use it.

## Three lines I'd refuse to bend on

1. **No I/O in domain.** Even "just one quick `os.Getenv` in this domain helper" — no. It's the seam.
2. **No `adapters/*` imports from `app/*` or `domain/*`.** Tempting shortcut for "just call `gh` from this use case" — never; route via `Provider.X` or a narrow interface. The CLI layer may still construct tool adapters directly.
3. **No leaky abstractions in `Provider`.** `Provider.GHRelease(...)` is wrong; `Provider.CreateRelease(spec ReleaseSpec)` is right. `ReleaseSpec` is a domain value type; the gh-vs-glab divergence lives entirely inside the adapters.

Holding those three is what keeps the codebase navigable at 50, 100, 200 commits in.

## Out-of-band: open questions

If specific Forgejo/Codeberg projects (`libs`, `arch`, etc.) have
conventions worth mirroring more strictly than this gommitlint-derived
shape, drop links and the layout can be patched. Otherwise this is what
the port should look like.
