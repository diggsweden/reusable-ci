<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Scripts

This document used to describe the bash scripts under `scripts/`. The workflow
entrypoints now call the `reusable-ci` Go binary instead. The remaining
`scripts/` directory is intentionally narrow:

| Path | Purpose |
|------|---------|
| `scripts/bootstrap/install-*.sh` | Toolchain installers invoked at runtime-image build time, plus `install-reusable-ci.sh` for plain/macOS runners that cannot use the runtime container. Renovate-managed pinning lives here where applicable. |

For the **CI surface** itself — every command a workflow can call — see
[docs/cli-reference.md](cli-reference.md). That file is auto-generated
from the binary's urfave/cli command tree by `cmd/gen-cli-reference`;
run `just gen-cli-reference` after any CLI change.

## Why a `reusable-ci` binary

In short:

1. **One Go binary, one surface.** Every workflow operation is a
   `reusable-ci <group> <subcmd>` call. The binary is baked into the
   runtime container; plain-runner workflows install it with `go install`
   when needed.
2. **Typed values, real errors, real tests.** The bash scripts were
   string-typed and hard to test; the Go port is structured around a
   domain/app/adapter split with one CLI binding per use case.
3. **Cross-provider shape.** The use cases are written behind provider-specific
   adapters. GitHub Actions workflows are shipped today; GitLab CI Catalog YAML
   remains tracked in [gitlab.prep.md](gitlab.prep.md).
