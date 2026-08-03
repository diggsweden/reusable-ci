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
problem for the verb lexicon (ADR 0003), env names, and single-sourced
regexes: write the rule down, then guard it with a test that explains the fix
in its failure message. The layering is the most consequential rule in the
codebase and it was the one rule with neither half.

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
