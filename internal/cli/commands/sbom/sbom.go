// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package sbom wires `reusable-ci sbom <subgroup> <subcmd>`.
//
// Two subgroups by intent:
//
//   - generate — produce SBOM artefacts via syft for one CISA layer
//     (artifacts, container) or all layers
//   - find     — locate an existing SBOM artefact on disk
package sbom

import "github.com/urfave/cli/v3"

// New returns the `sbom` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "sbom",
		Usage: "CISA-layered SBOM generation (SPDX + CycloneDX via syft)",
		Commands: []*cli.Command{
			generateGroup(),
			findGroup(),
		},
	}
}
