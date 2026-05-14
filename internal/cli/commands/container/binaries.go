// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func suffixBinariesCmd() *cli.Command {
	return &cli.Command{
		Name:      "suffix-extracted-binaries",
		Usage:     "rename extracted binaries with a -linux-<arch> suffix",
		ArgsUsage: "<dir> <arch> [expected-names]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() < 2 {
				return fmt.Errorf("Usage: suffix-extracted-binaries <dir> <arch> [expected-names]: %w", errs.ErrUsage)
			}
			return appcontainer.SuffixExtractedBinaries(os.Stdout, appcontainer.SuffixExtractedBinariesInput{
				Dir:           cmd.Args().Get(0),
				Arch:          cmd.Args().Get(1),
				ExpectedNames: cmd.Args().Get(2),
			})
		},
	}
}
