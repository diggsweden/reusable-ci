<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# ADR 0004: The internal package layering

Status: Proposed (2026-07-14)

## Context

`internal/` is laid out as ports and adapters: `domain`, `app`, `adapters`,
and `cli` sit side by side, with a handful of unlayered leaf utilities
(`listval`, `clicolor`, `cliio`, `safeexec`, `retry`, `archive`,
`runtimetags`, `testutil`) alongside them.

The layering is real and, at the time of writing, almost perfectly held:
`domain` imports nothing outward, `adapters` imports neither `app` nor `cli`,
and `app` does not import `cli`. That is the property most codebases claiming
this shape have already lost.

Three things were missing, and they compound:

1. **The rule is written down nowhere.** The words "hexagonal", "ports and
   adapters", and "dependency inversion" do not appear anywhere in the
   repository. A contributor deciding where a new file goes has to infer the
   rule from the existing tree, which works right up until it doesn't.

2. **Nothing enforces it.** `depguard` is not enabled in `.golangci.yml`.
   The clean import graph was held by discipline alone, so nothing fails CI
   the day someone adds `domain` → `adapters`.

3. **It had already started to drift.** `app/validate/pinreachability.go`
   constructed `&adaptergit.Repo{...}` inline, in two places. Its neighbour
   `app/validate/tags.go` takes a `gitOps` interface as a parameter and does
   it correctly, so the inconsistency was already live inside a single
   package. Because nothing checked, the wrong version was as easy to copy as
   the right one.

The `internal/cli/*_guard_test.go` family already solved this exact class of
problem for env names, commit-SHA and hex-digest spellings, and single-sourced
regexes: write the rule down, then guard it with a test that explains the fix
in its failure message. The layering is the most consequential rule in the
codebase and it was the one rule with neither half.

ADR 0003 §3 sets the bar a guard has to clear, and it is worth checking this
one against it rather than reaching for the family by analogy. A guard has to
pin a fact with one correct value, not a judgement with a long tail of
legitimate answers, and it earns its keep when the violation is expensive to
reverse. An import edge clears both tests where a verb does not. The edge is
present or it is not, with no tail to argue about; and a violation is not a
one-comment review fix, because once one use case constructs an adapter inline
the next one copies it. That is not hypothetical: it is how `pinreachability`
drifted while `tags.go` next door stayed correct.

## Decision

State the layering as a contract, guard it with a test, and fix the drift.

### 1. The layers and the arrows

Every arrow points inward at `domain`:

```text
cli  ──►  app  ──►  domain  ◄──  adapters
                      ▲
             adapters/platform
```

| Package | Role | May import |
|---|---|---|
| `domain` | Business rules and the ports (interfaces) that express what the rules need. Testable with no runner, network, or binary. | `domain`, leaf utilities |
| `app` | Use cases. Owns the ordering of steps; drives ports. | `domain`, leaf utilities |
| `adapters` | Driven side. Concrete implementations of ports: `git`, `cosign`, `syft`, `github`, `gitlab`, `platform`. | `domain`, leaf utilities |
| `cli` | Driving side. Flag parsing, rendering, and the composition root that constructs adapters. | anything |

The layers are **siblings on disk on purpose**. Hexagonal architecture has no
nesting hierarchy; the layering is expressed by import direction only.
Nesting `adapters/` under `domain/` would imply domain owns its adapters,
which is the exact dependency this design forbids, and Go grants a nested
package no privileged access anyway. `internal/` is already the visibility
boundary that matters.

### 2. Ports are declared consumer-side

A port is a **small interface declared next to the code that uses it**,
listing only the methods that caller needs, not a mirror of the adapter's
full surface. `gitOps` in `app/validate/tags.go` is the reference example.
There is no `ports/` package, and there will not be one: that is a Java habit
that Go's structural typing makes unnecessary.

Adapters do not import the interface they satisfy. They satisfy it
structurally, which is what keeps the arrow pointing inward.

### 3. Leaf utilities are outside the stack

`listval`, `clicolor`, `cliio`, `safeexec`, `retry`, `archive`,
`runtimetags`, and `testutil` carry no domain knowledge and belong to no
layer. Any layer may import them. `listval` is imported by `domain` itself,
which is precisely why it cannot live inside one of the layered trees. They
stay at the top of `internal/`.

### 4. `platform` is an adapter

`platform` reads `os.Getenv` to decide which forge and runner the binary is
running on. Reading the environment is I/O, which makes it a driven adapter
by the same rule that places `git` and `cosign`. It moves from
`internal/platform` to `internal/adapters/platform`. The package name and API
are unchanged; only the import path moves.

### 5. Pure adapter functions are not ports

`app` may call a function in an adapter package when that function is pure:
takes bytes, returns a value, touches no file, network, or subprocess.
`openpgp.VerifyDetachedArmored` and `openpgp.PrimaryFingerprints` qualify.
Calling one is using a library, not driving a port, so there is nothing to
inject and nothing to fake.

This is a narrow exemption, not a loophole. The same `adapters/openpgp`
package exposes a `*Signer` whose methods do touch the filesystem; `app` code
must reach those through a port like every other adapter. The exemption is
listed explicitly in the guard, so widening it is a reviewable act.

### 6. The guard

`internal/archguard/layering_guard_test.go` walks the import graph under
`internal/` and fails on any outward edge:

- `domain` → `app`, `adapters`, `cli`
- `adapters` → `app`, `cli`
- `app` → `adapters` (except the pure-function allowlist), `cli`

It uses the standard library's `go/parser` rather than `golang.org/x/tools`,
which is not a direct dependency and should not become one for a test.

Test files are exempt. Test code is a composition root: `app/validate`'s own
tests legitimately wire the real git adapter to exercise real reachability
semantics, and a fake there would only restate the answer under test.

The guard lives in its own directory rather than beside the `internal/cli`
guards because it inspects the whole tree, not the CLI command surface.

## Consequences

- The rule a contributor needs is written down, and the guard names the fix
  in its failure message rather than restating the rule.
- `pinreachability.go` now takes a `PinGit` port. Because the clone
  destination is only known at runtime, the port is a factory (`Clone` +
  `Open`) rather than a ready-made repo, which preserves the existing
  laziness: no clone happens when a workflow has no pins to check.
- `PinReachabilityInput.GitBin` is removed. It existed only to construct the
  adapter, and nothing set it.
- The CLI shim's `Open` returns the `PinGitOps` port rather than `*git.Repo`,
  because Go has no covariant returns. That indirection is the inversion, and
  it costs one small type in the composition root.
- Cost: `app` → `adapters` is now a guarded edge, so a use case that needs a
  new tool must declare a port and wire the adapter in `internal/cli`. That
  is a real speed bump on the "just call it" path, and it is the point.
- The layering can no longer erode silently, which is what makes the current
  state worth freezing now rather than after it drifts further.

## Update (2026-08-11): the reasoning applied to the rest of the family

§6 justified giving the layering guard its own directory: a guard that
inspects the whole tree does not belong beside the CLI command surface. That
argument was never acted on for the eighteen other guards in `internal/cli`,
all of which resolve the repository root and read `docs/`, `.github/`, or the
whole tree. `internal/cli` had four production files and thirty test files,
most of which had nothing to say about the CLI.

They now sit in packages named for what each is answerable for. The current
set, and the rule for choosing between them, is the guard table in
[`docs/testing.md`](../testing.md) — deliberately not repeated here, so that
adding a guard package means editing one document rather than remembering
this one. `TestGuardPackagesAreDocumented` holds that table to the tree.

Nothing about the layering changed; the decision above stands as written.
Three consequences worth recording:

- `internal/testutil/reporoot` is the single source for "where is the
  repository root", which the guards previously duplicated.
- `internal/testutil/cliflags.Sources` replaces two near-identical reflection
  helpers for reading a flag's value sources. The surviving version fails the
  test when it cannot read a flag rather than returning an empty chain: for a
  guard hunting env vars in the wrong place, "I could not read this" and
  "this reads nothing" are opposite answers.
- The single-source guards exclude their own declaring file by path, so
  moving them required updating those exclusions. That they failed loudly on
  the first run is the guards working.
