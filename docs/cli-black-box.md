<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# CLI black-box testing requirements

Behaviour-only test requirements for the `reusable-ci` binary. Scenarios are
ID'd and stated as **observable results** so they can be automated, tracked,
and release-gated. These tests run the *built* binary as an external process
and assert only what a workflow step, a CI runner, or an operator can see:
exit code, stdout, stderr, files written (`$GITHUB_OUTPUT`,
`$GITHUB_STEP_SUMMARY`, artifact dirs, auth config), forge API calls against a
loopback mock, and child-tool argv.

This complements — does not replace — [`docs/testing.md`](testing.md) (the
unit/adapter/e2e layering) and the domain unit tests that own internal
algorithms. The black-box layer preserves the **public CLI contract** that the
reusable workflows in `.github/workflows/` depend on, across releases and
across forges.

There are two harnesses, and this document is the catalogue and the
acceptance criteria for both:

- `cmd/reusable-ci/e2e_test.go` (build tag `e2e`, `just test-e2e`) — the
  in-repo smoke harness. It needs nothing installed, so it covers the root
  contract: command discovery, help, version, flag parsing, the exit-code
  ladder, stream discipline.
- [`diggsweden/reusable-ci-blackbox-tests`](https://github.com/diggsweden/reusable-ci-blackbox-tests)
  — the companion testsuite, and where most of the scenarios below actually
  live. It drives the built binary against committed fixture projects with
  the real toolchains (`syft`, `gpg`, `cosign`, `mvn`, `gradle`, `cargo`,
  `npm`) and a faked network.

A new scenario goes in the testsuite if it needs a real toolchain or a
fixture project, and here if it is about the CLI's own surface. Note that
the testsuite calls itself the *integration* tier and reserves "e2e" for a
real runner against a real forge; the `e2e` build tag in this repository
means the smoke harness only.

## Scope

In scope:

- Root contract: command discovery, help, version, flag parsing (`--flag value`
  vs `--flag=value`), unknown-command suggestions, the exit-code ladder, and
  stdout/stderr stream discipline.
- The command groups invoked by workflows: `artifact`, `build`, `config`,
  `container`, `doctor`, `plan`, `platform`, `publish`, `release`, `report`,
  `sbom`, `security`, `validate`, `version`.
- Output contract: `--format` (`auto`/`text`/`json`/`github`/`gitlab`),
  `--json`, `--no-color`, and the CI sinks (`$GITHUB_OUTPUT`,
  `$GITHUB_STEP_SUMMARY`, GitLab dotenv/sections).
- Forge portability: the same verb resolving to GitHub, GitLab, Forgejo, or
  local behaviour under `--provider` / `REUSABLE_CI_PROVIDER` and runner
  detection.
- Domain correctness exposed through the CLI: validation gates, tag/version
  rules, container name/metadata/ledger, SARIF category stamping, artifact
  upload/download flag semantics, release metadata.
- Security and safety: secrets never in argv, secrets never logged, SARIF /
  CI-annotation escaping, zip/path-traversal refusal, and no host effects.

Out of scope:

- Internal algorithms and helpers already covered by domain unit tests.
- TUI behaviour — `reusable-ci` is intentionally a non-interactive CI CLI.
- Live runs against real forges, registries, or the GitHub Actions artifact
  "results" backend. Those need a real runner and are explicitly opt-in, never
  part of this hermetic plan or CI (see HAR-05/06 and the off-runner note under
  `ART-*`).

## Exit-code contract (asserted throughout)

The ladder is BSD `sysexits.h`-aligned for codes ≥64; POSIX `1`/`2` keep their
conventional meaning. Source of truth: `internal/domain/errs`
(`ExitCodeFromError`).

| Code | Name | Meaning |
|---|---|---|
| `0` | OK | success, including `--help` / `--version` and passing gates |
| `1` | Validation | a rule/gate failed (e.g. bad tag, doctor FAIL, lint gate) |
| `2` | Usage | CLI misuse: bad flags, wrong args, unknown command, context-cancel |
| `65` | DataErr | malformed input (bad JSON/YAML payload, unparsable value) |
| `66` | NoInput | a required input file/ref/artifact is missing |
| `69` | Unavailable | external dependency missing/unreachable, rate-limited, timed out |
| `70` | Software | unclassified internal error or panic boundary |
| `77` | NoPerm | auth / permission failure (missing secret, 401/403) |
| `78` | Configuration | bad config file (schema violation, invalid `artifacts.yml`) |

HTTP status mapping (`errs.FromHTTPStatus`): `401`/`403`→`77`, `404`→`66`,
`429`→`69`, `5xx`→`69`.

## Test harness requirements

The suite must **in no way affect the host OS, the user's data, real
credentials, or any real network resource**. Anything that reads host config,
mutates global/SCM/GPG config, touches a real keyring, or reaches a real forge
is a **harness failure**, not a valid result. reusable-ci already ships the
isolation primitives these requirements need; the table maps each to the
helper that satisfies it.

| ID | Requirement | Helper |
|---|---|---|
| HAR-01 | Run the freshly built binary as an external process. Do not import internal packages into the assertion. | `buildBinary(t)` + `runBinary(t, …)` in `cmd/reusable-ci/e2e_test.go` (builds once via `TestMain`). |
| HAR-02 | Create `HOME`, every XDG base, configs, output/summary files, artifact dirs, and any keyring under one isolated temp dir; delete it afterwards. | `testenv.New(t)` → `isolatedenv.Isolate(t)` (sets `HOME`, `USERPROFILE`, `XDG_{CONFIG,DATA,CACHE,STATE}_HOME`, `TMPDIR`); `testfs.NewReal(t)` for fixture files. |
| HAR-03 | Neutralize ambient Git/GPG/SSH state. | `isolatedenv` sets `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, fixed `GIT_AUTHOR/COMMITTER_*`, `GIT_TERMINAL_PROMPT=0`, empty `GIT_ASKPASS`/`SSH_AUTH_SOCK`, `GNUPGHOME` under temp. |
| HAR-03b | Neutralize ambient **CI/forge/token** state so a test (and any binary it spawns with `os.Environ()`) never inherits the developer's or the runner's real forge context. | `isolatedenv` scrubs every `GITHUB_*`/`ACTIONS_*`/`RUNNER_*`/`GITLAB_*`/`CI_*`/`FORGEJO_*`/`GITEA_*`/`REUSABLE_CI_*`/`ARTIFACT_*`/`REGISTRY_*` var plus standalone `CI`, `GH_TOKEN`, `DOCKER_CONFIG`, `GPG_PRIVATE_KEY`, `COSIGN_*`, … to empty (detection reads empty as absent). A test that needs one re-sets it *after* `Isolate`. Locked by `TestIsolate_ScrubsAmbientCIEnv`. |
| HAR-04 | Pin locale and disable colour for default assertions; opt into colour/locale only where under test. | Set `LANG=C.UTF-8 LC_ALL=C.UTF-8` and `--no-color` / `NO_COLOR=1` (or `REUSABLE_CI_NO_COLOR`) in the scenario env. |
| HAR-05 | Network is forbidden except a **loopback mock forge**. Forge-API scenarios point the adapter at `127.0.0.1`. | `fakegitserver` (GitHub REST) / `fakegitlabserver` (GitLab) via `APIBaseOverride`; `isolatedenv` empties all `*_proxy` vars so a stray real call cannot be proxied out. |
| HAR-06 | Drive auth with fake tokens only; never the host keyring. | Export disposable tokens (`GITHUB_TOKEN=dummy`, `REGISTRY_PASSWORD=…`) into the scenario env; assert they are absent from stdout/stderr/written files. |
| HAR-07 | Capture stdout, stderr, exit code separately for every command; assert on stable machine output, not human prose, where a JSON/sink field exists. | `runBinary` returns `(stdout, stderr, exit)`. |
| HAR-08 | Exercise both `--flag value` and `--flag=value` for string/int flags; assert equivalent results. | Scenario matrix (see `CLI-05`). |
| HAR-09 | Force the CI runtime explicitly rather than depending on the host. | `--provider {github,gitlab,forgejo,local}` / `REUSABLE_CI_PROVIDER`, `--runner …`; CI env fixtures `ghaenv` / `glabenv`. |
| HAR-10 | "No writes / no deletes" scenarios snapshot the temp tree (and the mock's recorded calls) before and after, and diff. | `testfs` + mock-server call recorder. |
| HAR-11 | Performance scenarios record input size, wall-clock, and machine class so a regression is explainable. | n/a (scheduled hardening only). |

Reference sandbox for a non-Go harness (CI shell), satisfying HAR-02/03/04 —
wrap in a network sandbox per HAR-05:

```sh
root="$(mktemp -d)"
export HOME="$root/home"
export XDG_CONFIG_HOME="$root/config" XDG_STATE_HOME="$root/state"
export XDG_DATA_HOME="$root/data"     XDG_CACHE_HOME="$root/cache"
export GIT_CONFIG_GLOBAL="$root/gitconfig" GIT_CONFIG_NOSYSTEM=1
export GNUPGHOME="$root/gnupg" TMPDIR="$root/tmp"
export LANG=C.UTF-8 LC_ALL=C.UTF-8 NO_COLOR=1
export GITHUB_TOKEN=dummy        # fake creds → loopback mock only
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$GNUPGHOME" "$TMPDIR"
# … run reusable-ci scenarios, writing only under "$root" …
rm -rf "$root"                   # leave no trace on the host
```

## Fixture catalogue

Deterministic fixtures the harness builds under the temp root. Forge fixtures
are served on loopback with a fixed, known resource set so expected counts/IDs
are exact.

| ID | Fixture | Purpose |
|---|---|---|
| FIX-01 | Empty non-repo temp dir, no `.reusable-ci/`. | Help/version, no-config diagnostics, commands that must not require a repo, `doctor` missing-artifacts FAIL. |
| FIX-02 | Minimal repo with `.reusable-ci/artifacts.yml` (one `meta` artifact, default sign). | `doctor` clean path, config discovery, read-only command checks. |
| FIX-03 | `artifacts.yml` variants: missing, malformed YAML, schema-invalid, `sign.method: sigstore` without id-token. | Config-error (`78`), validation (`1`), and remediation-message scenarios. |
| FIX-04 | Workflow fixtures under `.github/workflows/`: literal-default input, expression-default input, missing event-context guard. | `validate workflow input-defaults`, privileged-workflow invariants. |
| FIX-05 | Loopback GitHub mock (`fakegitserver`) seeded with known releases, run artifacts (varied names for glob), and SARIF upload endpoints. | Release create/download, `artifact download` pattern/merge, `security report upload-sarif`. |
| FIX-06 | Loopback GitLab mock (`fakegitlabserver`). | GitLab provider parity for the same verbs. |
| FIX-07 | Mock returning `401`/`403`, `404`, `429`, `5xx` on selected endpoints. | Exit-code mapping (`77`/`66`/`69`) and actionable hints. |
| FIX-08 | Inputs containing control chars, ANSI/OSC, CR/BS, newlines, and Unicode (commit subjects, artifact names, SARIF messages, branch names). | Terminal-safety + JSON/SARIF/annotation escaping. |
| FIX-09 | CI env overlays: `ghaenv` (GitHub Actions), `glabenv` (GitLab CI), Forgejo runner vars, plain/local. | Forge + runner detection and sink selection. |
| FIX-10 | Filesystem trees for artifact globs: `dist/**`, `*.json`, `**/bom.json`, dotfiles, a bare directory, `!exclude` cases. | `artifact upload --path` glob/LCA-root/include-hidden semantics. |
| FIX-11 | Temp paths containing spaces, Unicode, and shell metacharacters as literal chars. | Path handling and shell-safety. |

## CLI-* — Global contract & composability

| ID | Scenario | Expected observable result |
|---|---|---|
| CLI-01 | `reusable-ci` with no args. | Exit `0`; concise help/command list on **stdout**; stderr empty; no stack trace; no side effects. |
| CLI-02 | `--help`, `help`, and a representative `<group> --help`. | Exit `0`; help on **stdout**; each group page names only its own commands and flags; pipeable. |
| CLI-03 | `--version`. | Exit `0`; stdout has version, commit, build date; works with no repo, config, token, or network. |
| CLI-04 | Unknown command, e.g. `reusable-ci bogus-cmd`. | Exit `2`; stderr `unknown subcommand '…'. Did you mean "…"?`; root usage; never auto-runs the guess. |
| CLI-05 | `--flag=value` vs `--flag value` on any value flag (e.g. `--log-level`). | Identical exit code and output for both forms. |
| CLI-06 | Unexpected positional args / unknown flag on a leaf command. | Exit `2`; error explains the command shape; nothing else runs. |
| CLI-07 | Stream discipline: `<machine cmd> 2>/dev/null` and `1>/dev/null`. | Machine/data output only on stdout; human diagnostics, progress, and logs only on stderr. |
| CLI-08 | Every command with stdin closed / from an empty pipe. | No command blocks waiting for input; completes or fails with an actionable error (the CLI is non-interactive). |
| CLI-09 | Invalid global enum values: `--log-level`, `--format`, `--provider`, `--runner` set to a bogus value. | Exit `2`; stderr names the accepted values. |
| CLI-10 | SIGPIPE: pipe a machine-output command into a consumer that exits early (`… \| head -1`). | Clean exit; no panic, stack trace, or noisy broken-pipe diagnostic. |

## OUT-* — Output formats, streams, and CI sinks

| ID | Scenario | Expected observable result |
|---|---|---|
| OUT-01 | A command that emits machine data with `--json` / `--format json`. | stdout is valid JSON; logs/diagnostics go to stderr; stdout parses without filtering. |
| OUT-02 | A gate that emits CI annotations (e.g. `validate workflow input-defaults`, `validate workflow contract-residue`, `validate event-context`) on failure, on a GitHub vs non-GitHub runner. | These now route through the format-aware `output.Annotator`: on a GitHub runner they emit `::error file=…,line=…::…` workflow commands with **every interpolated value escaped** (`EscapeWorkflowCommandData`/`Property`) so hostile scanned content cannot forge a command; on a non-GitHub runner they emit a plain `Error: <file>:<line>: …`/`Warning: …` with no workflow-command noise. This now covers **every** annotation site (including `tags.go`'s allowlist `::warning::` notices) — no raw workflow-command `Fprintf` remains. |
| OUT-03 | A command that sets step outputs (e.g. `release resolve-release-metadata`) with `GITHUB_OUTPUT` pointed at a temp file. | The `key=value` lines are appended to `$GITHUB_OUTPUT`; values are correctly escaped; no duplicate copy on stdout. |
| OUT-04 | A command that writes a job summary (e.g. `summary quality-check-status`) with `GITHUB_STEP_SUMMARY` pointed at a temp file. | Markdown is appended to the summary file; an invalid/relative summary path is ignored safely (no crash). |
| OUT-05 | Colour: default piped output, `--no-color`, `NO_COLOR=1`. | Piped/`--no-color`/`NO_COLOR` output has no ANSI; the `✓/✗` markers degrade to plain text. |
| OUT-06 | Determinism: run the same machine-output command twice on identical state. | Byte-identical stdout except documented timestamps; stable field order. |

## FORGE-* — Forge & runner portability

| ID | Scenario | Expected observable result |
|---|---|---|
| FORGE-01 | A verb under `--provider github`, `gitlab`, `forgejo`, and `local`. | The verb resolves to the matching adapter; an operation a forge cannot perform fails with a typed "not supported" (`69`), never a silent wrong result. |
| FORGE-02 | Forge auto-detection from env (`ghaenv` / `glabenv` / Forgejo runner vars). | The detected forge matches the runtime; Forgejo is detected before GitHub when both env families are present (Forgejo runners set both). |
| FORGE-03 | Sink selection by runner: `$GITHUB_OUTPUT` under GHA-compatible runners, GitLab dotenv under GitLab. | Step outputs land in the runner's native sink; the same verb is portable across runners. |
| FORGE-04 | `REUSABLE_CI_PROVIDER` / `--provider` override vs detected env. | The explicit override wins over detection. |

## ART-* — Artifact upload/download CLI contract

`artifact upload` drives the GitHub Actions v4 "results" backend (via the
runner-provided `ACTIONS_RUNTIME_TOKEN` / `ACTIONS_RESULTS_URL`) and is
**untestable off-runner** — the transport itself must be proven on a real CI
run. Everything below is the *deterministic*, off-runner-testable contract:
argument validation and the file-collection semantics that resolve **before**
any transport. The collection logic (`internal/domain/artifact`) is also
covered by domain unit tests; these assert it through the binary.

| ID | Scenario | Expected observable result |
|---|---|---|
| ART-01 | `artifact upload --name x` with no `--dir`/`--path`/`--file`. | Exit `2`; `upload needs --dir, --path, or at least one --file`. |
| ART-02 | `artifact download --name a --pattern 'b*' --dir d`. | Exit `2`; `use --name or --pattern, not both`. |
| ART-03 | `artifact download` with neither `--name` nor `--pattern`. | Exit `2` (name fails validation); message names the missing selector. |
| ART-04 | `artifact upload --name x --path '<empty-glob>' --if-no-files ignore`. | Exit `0`; `Uploaded x (0 files, 0 bytes)`; no transport attempted. |
| ART-05 | `artifact upload --name x --path '<empty-glob>'` (default `--if-no-files error`). | Exit `1`; `no files matched for artifact "x"`. |
| ART-06 | `artifact upload --path 'dir/**'` over FIX-10 — assert collected set via probe/log. | Glob expands `**` across segments; directory structure is preserved relative to the least-common-ancestor root; dotfiles excluded unless `--include-hidden`; `!`-prefixed lines exclude. |
| ART-07 | `artifact download --pattern 's*' [--merge-multiple]` against FIX-05 mock. | Matching artifacts download into `--dir/<name>/`, or flattened into `--dir` with `--merge-multiple`; zero matches is a no-op, not an error. |
| ART-08 | Zip-traversal artifact (`../escape`) downloaded against FIX-05. | Refused with `65`; nothing is written outside the destination dir (SafeJoin). |
| ART-09 | The same `upload`/`download` surface under `--provider forgejo`. | Identical flags and behaviour as GitHub (forge-neutral run-artifact store). |

## VAL-* — Validation & policy gates

| ID | Scenario | Expected observable result |
|---|---|---|
| VAL-01 | `validate ref-type` with a non-tag ref on a release flow. | Exit `1`; stderr explains the release workflow must be triggered by a tag. |
| VAL-02 | `validate tag format --tag 1.0.0` (no `v`). | Exit `1`; `Invalid tag format`. |
| VAL-03 | `validate auth registry` with an empty required `REGISTRY_PASSWORD`. | Exit `77`; `registry-password secret is required`. |
| VAL-04 | `validate workflow input-defaults` over FIX-04 literal vs expression default. | Literal → exit `0`; expression default → exit `1` with a `::error file=…,line=…::` annotation. |
| VAL-05 | `validate event-context` under a `pull_request` event with secrets in scope. | Refuses with the documented exit code; the message names the PR-context risk. |
| VAL-06 | `doctor` over FIX-02 (clean) / FIX-01 (missing) / FIX-03 (sigstore w/o id-token). | Clean → exit `0`, `[OK]` lines, no `[FAIL]`; missing/invalid → exit `1` with a `[FAIL]` line and concrete remediation (`id-token: write`). |
| VAL-07 | Malformed `artifacts.yml` (bad YAML / schema). | `config validate <file>` → exit `78` (configuration), message points at the file. `doctor` over the same file → exit `1`: it intentionally aggregates every check failure to "N failing check(s)" (validation), so `78` is asserted against a direct consumer, not doctor. |

## CON-* / REL-* — Container & release domain through the CLI

| ID | Scenario | Expected observable result |
|---|---|---|
| CON-01 | `container login` with `REGISTRY_*` set, `$REGISTRY_AUTH_FILE` under temp. | Writes `{"auths":{"<reg>":{"auth":"<base64>"}}}` at `0600`; the password never appears in argv, stdout, stderr, or logs. |
| CON-02 | `container login` precedence: `$REGISTRY_AUTH_FILE` → `$DOCKER_CONFIG/config.json` → `~/.docker/config.json`. | The first set path wins; the dir is created `0700`. |
| CON-03 | `container ledger add` / `ledger merge` over a temp ledger tree. | The merged ledger reflects every added entry; merging a missing path is a graceful no-op. |
| CON-04 | `security report upload-sarif` against FIX-05 with `--category`. | The category is stamped into each run's `automationDetails.id`; the POST is gzip+base64; a `5xx` maps to `69`, `401/403` to `77`. |
| REL-01 | `release resolve-release-metadata` with `VERSION`/`REPOSITORY`/`ARTIFACT_NAME`. | `$GITHUB_OUTPUT` gets `version=…`, `version-no-v=…`, `project-name=…`. |
| REL-02 | `release create` / `download-artifacts` against FIX-05 mock. | Release is created/updated; planned artifacts download; a missing release maps to `66`. |

## SEC-* — Security & safety

| ID | Scenario | Expected observable result |
|---|---|---|
| SEC-01 | Grep stdout/stderr/written files/`--log-level debug` for the token/password after a full `container login` / forge run. | Zero matches anywhere; secrets are never in argv, logs, outputs, or the on-disk auth config beyond the documented base64 auth blob. |
| SEC-02 | Secrets are only accepted via env/stdin/file, never as a flag/positional. | No `--password`/`--token` value flag exists that would land the secret in argv. |
| SEC-03 | FIX-08 hostile text through `--format github`/`gitlab` and `--json`. | Annotations escape `::`, `%0A`, `%0D`, newlines so input cannot forge workflow commands; JSON stays valid and escaped; no terminal control bleed. |
| SEC-04 | Path traversal: artifact zip `../escape`, `--path ../…`, output/summary paths outside temp. | Rejected or confined; nothing is written outside the intended/temp locations (SafeJoin / path validation). |
| SEC-05 | No implicit host state: run with a hostile `~/.docker/config.json`, global git config, and GPG home present on the (isolated) host. | Only the isolated temp env is read/written; user/system config is untouched. |
| SEC-06 | Shell-metacharacter inputs in refs, names, paths (FIX-11). | Treated as literal data or rejected; no shell command executes because of user data (no `sh -c` on untrusted input). |

## STAB-* — Stability & determinism

| ID | Scenario | Expected observable result |
|---|---|---|
| STAB-01 | Run the same read-only command 100× on identical state. | Exit code and output stable except documented timestamps. |
| STAB-02 | Concurrent runs across independent temp envs. | No cross-leak via HOME/XDG/auth config/sinks; outputs not corrupted. |
| STAB-03 | Panic boundary: trigger an unclassified internal fault. | Exits `70` with internal-error text on stderr; no Go default panic leak. |
| STAB-04 | Context cancellation / timeout against a slow FIX-12 mock. | A cancel maps to `2`; a dependency timeout maps to `69` (not `70` — a slow registry is not a bug). |
| STAB-05 | No-network posture (HAR-05). | No command except those explicitly hitting the loopback mock attempts network; none waits on a real-network timeout. |

## DIST-* — Documentation & distribution parity

| ID | Scenario | Expected observable result |
|---|---|---|
| DIST-01 | Compare `--help` / group help against `docs/cli-reference.md`. | Commands, flags, and defaults agree. Enforced by `TestDocsCLIReferenceInSync` + `just gen-cli-reference`. |
| DIST-02 | Workflow ↔ CLI parity: every `reusable-ci <verb>` invoked in `.github/workflows/` resolves to a real command with the flags/env the workflow passes. | No drift between workflow call sites and the CLI surface. |
| DIST-03 | Version-metadata parity for a release-style build (ldflags injected) vs a bare `go build`. | A release build reports the real version; a bare build's `dev`/empty version must not mask drift in release smoke checks. |

## Minimum release gate

A candidate should not ship unless these groups pass on at least one supported
Linux architecture (hermetic fixtures, loopback mocks, fake secrets):

- `CLI-01`–`CLI-10`
- `OUT-01`–`OUT-06`
- `FORGE-01`–`FORGE-04`
- `ART-01`–`ART-09`
- `VAL-01`–`VAL-07`
- `CON-01`–`CON-04`, `REL-01`
- `SEC-01`–`SEC-06`
- `STAB-03`, `STAB-04`, `STAB-05`
- `DIST-01`, `DIST-02`

The remainder (100× stability, concurrency, large-input performance,
resource-limit handling, full arch/package matrix) runs in scheduled or
pre-release hardening jobs.

## Traceability

| Source | Contract used by this plan |
|---|---|
| `docs/cli-reference.md` | Command syntax, flags, env vars, defaults. |
| `internal/domain/errs` (`ExitCodeFromError`, `FromHTTPStatus`) | The exit-code ladder and HTTP-status mapping. |
| `docs/workflow-design-policy.md` | Node-less/forge-portable goal, event-context guard invariant, artifact verb contract. |
| `docs/testing.md` | Layer boundaries, build tags, and the `e2e` harness location. |
| `docs/providers.md` | Forge detection and the per-provider capability matrix. |
| `docs/artifacts-reference.md` | `artifacts.yml` schema that `doctor`/`config` validate. |
| `cmd/reusable-ci/e2e_test.go` | The in-repo smoke harness: root contract, help, version, exit-code ladder. |
| [`diggsweden/reusable-ci-blackbox-tests`](https://github.com/diggsweden/reusable-ci-blackbox-tests) | The bulk of these scenarios: real toolchains, fixtures, signing round-trips, reproducibility. |
