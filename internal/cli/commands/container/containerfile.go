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

func validateContainerfileCmd() *cli.Command {
	return &cli.Command{
		Name:      "validate-containerfile",
		Usage:     "verify a Containerfile path exists (or glob-resolves uniquely), emit containerfile=<path>",
		ArgsUsage: "<containerfile>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("Usage: validate-containerfile <containerfile>: %w", errs.ErrUsage)
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()
			return appcontainer.ValidateContainerfile(ctx, d.OutputSink, os.Stdout, appcontainer.ValidateContainerfileInput{
				Path: args[0],
			})
		},
	}
}
