// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func validateArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:      "validate-artifacts",
		Usage:     "verify project-type-specific artifacts are present, warn on COPY-instead-of-rebuild policy",
		ArgsUsage: "<project-type> <artifact-dir> [containerfile-path]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 2 {
				return fmt.Errorf("Usage: validate-artifacts <project-type> <artifact-dir> [containerfile-path]: %w", errs.ErrUsage)
			}
			annot := deps.Annotator(cmd)
			return appcontainer.ValidateArtifacts(os.Stdout, os.Stderr, annot, appcontainer.ValidateArtifactsInput{
				ProjectType:       cmd.Args().Get(0),
				ArtifactDir:       cmd.Args().Get(1),
				ContainerfilePath: cmd.Args().Get(2),
			})
		},
	}
}
