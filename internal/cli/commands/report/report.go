// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package report wires `reusable-ci report ...` subcommands that
// write to the platform's step-summary surface.
//
// Subcommand tree:
//
//   - build.go     — `build <ecosystem>` per-ecosystem build summaries
//   - publish.go   — `publish <target>` per-target publish summaries
//   - status.go    — `status <stage|prerequisites|build-sbom|sbom-count|quality-check>`
//   - lifecycle.go — flat lifecycle: `release`, `snapshot-release`, `pr`
//   - misc.go      — flat one-offs: `extracted-binaries`, `swift-lint`
package report

import (
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cmdmeta"
)

// New returns the `report` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "report",
		Usage: "write step-summary blocks (build, publish, status, lifecycle)",
		Commands: slices.Concat(
			cmdmeta.WithCategory("Build & lint", buildGroup(), extractedBinariesCmd(), swiftLintCmd()),
			cmdmeta.WithCategory("Publish & release", publishGroup(), releaseCmd(), snapshotReleaseCmd()),
			cmdmeta.WithCategory("PR & status", prCmd(), statusGroup(), stageResultCmd(), jobResultCmd()),
		),
	}
}
