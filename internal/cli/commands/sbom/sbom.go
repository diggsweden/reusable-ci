// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package sbom wires `reusable-ci sbom <subcmd>`.
//
// Commands by intent:
//
//   - assemble — the one assembly verb, three modes by flag/env: single-project
//     (harvest-or-generate the build BOM + syft-scan artifacts/containers),
//     --plan (one layer set per artifact in a config plan), and container
//     (--image-name/--image-digest for a multi-artifact analyzed-container SBOM)
//   - build    — the build-layer GENERATE tier: run the ecosystem's native
//     CycloneDX tool (go/cargo) to emit the build bom.json on a toolchain runner
//
// All SBOM concerns live here, not split across `build`; `build <eco>` only
// builds artifacts (its `run` still emits the build BOM as a byproduct).
package sbom

import "github.com/urfave/cli/v3"

// flagWorkingDir is the shared --working-dir flag name across the sbom commands.
const flagWorkingDir = "working-dir"

// New returns the `sbom` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "sbom",
		Usage: "CISA-layered SBOM tooling (SPDX + CycloneDX via syft)",
		Description: `Which SBOM verb do I want?

   assemble   PRODUCE a CISA layer set. One verb, three modes by flag/env:
              (default)        single project — harvest-or-generate the build BOM
                               + syft-scan artifacts/containers (--layers all).
              --plan JSON      per-artifact matrix over a config plan (CI).
              --image-name …   multi-artifact analyzed-container SBOM (CI).
   build      GENERATE just the build-layer bom.json with the native tool
              (go/cargo) — the toolchain-runner generate tier ` + "`sbom assemble`" + ` harvests.

Related, outside this group: ` + "`release sbom-zip`" + ` bundles assembled layers into a release zip.`,
		Commands: []*cli.Command{
			assembleCmd(),
			buildCmd(),
		},
	}
}
