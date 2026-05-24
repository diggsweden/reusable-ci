// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
)

func suffixBinariesCmd() *cli.Command {
	return &cli.Command{
		Name:  "suffix-extracted-binaries",
		Usage: "rename extracted binaries with a -linux-<arch> suffix",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "dir",
				Required: true,
				Sources:  cli.EnvVars("EXTRACTED_BINARIES_DIR"),
				Usage:    "directory holding the extracted binaries",
			},
			&cli.StringFlag{
				Name:     "arch",
				Required: true,
				Sources:  cli.EnvVars("ARCH"),
				Usage:    "target CPU architecture appended to each binary name (e.g. amd64, arm64)",
			},
			&cli.StringFlag{
				Name:    "expected-names",
				Sources: cli.EnvVars("EXPECTED_BINARY_NAMES"),
				Usage:   "comma-separated allow-list of base filenames to rename (default: all binaries in --dir)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appcontainer.SuffixExtractedBinaries(os.Stderr, appcontainer.SuffixExtractedBinariesInput{
				Dir:           cmd.String("dir"),
				Arch:          cmd.String("arch"),
				ExpectedNames: cmd.String("expected-names"),
			})
		},
	}
}
