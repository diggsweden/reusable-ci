<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Scripts

This document used to describe the bash scripts under `scripts/`. As of
Phase 12 of the Go port (see `docs/go-port-plan.md`), the bash scripts
that workflows called have been replaced by the `reusable-ci` Go
binary. The remaining `scripts/` directory is intentionally narrow:

| Path | Purpose |
|------|---------|
| `scripts/bootstrap/install-*.sh` | Toolchain installers invoked at runtime-image **build time** (yq, gh, glab, mise, opengrep, syft, trivy, git-cliff, publiccode-parser). Renovate-managed pinning lives here. |

For the **CI surface** itself — every command a workflow can call — see
[docs/cli-reference.md](cli-reference.md). That file is auto-generated
from the binary's urfave/cli command tree by `cmd/gen-cli-reference`;
run `just gen-cli-reference` after any CLI change.

For the migration history that brought us here, see
[docs/go-port-plan.md](go-port-plan.md).

## Why a `reusable-ci` binary

The three-line summary from the port plan:

1. **One Go binary, one surface.** Every workflow operation is a
   `reusable-ci <group> <subcmd>` call. The binary is baked into the
   runtime container; plain-runner workflows install it with `go install`
   when needed.
2. **Typed values, real errors, real tests.** The bash scripts were
   string-typed and hard to test; the Go port is structured around a
   domain/app/adapter split with one CLI binding per use case.
3. **Cross-provider.** The same use case runs against GitHub Actions
   today and GitLab CI in the same shape — the provider-specific
   adapters are the seam, the use cases never know.
