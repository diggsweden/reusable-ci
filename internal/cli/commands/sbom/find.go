// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appsbom "github.com/diggsweden/reusable-ci/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// findGroup wires `reusable-ci sbom find <kind>` — locate an existing
// SBOM artefact on disk and emit its path through the output sink.
// Currently only the analyzed-container layer is searchable; more
// kinds (artifacts, build) land here when needed.
func findGroup() *cli.Command {
	return &cli.Command{
		Name:  "find",
		Usage: "locate an existing SBOM artefact on disk",
		Commands: []*cli.Command{
			findContainerCmd(),
		},
	}
}

func findContainerCmd() *cli.Command {
	return &cli.Command{
		Name:  "container",
		Usage: "find a *-analyzed-container-sbom.spdx.json file in cwd, emit sbom-file=<basename>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appsbom.FindContainerSBOM(ctx, d.OutputSink, os.Stderr, os.Stderr, annot, appsbom.FindContainerSBOMInput{})
			})
		},
	}
}
