// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
)

func extractNPMTarballCmd() *cli.Command {
	return &cli.Command{
		Name:  "extract-npm-tarball",
		Usage: "extract a top-level *.tgz / *.tar.gz with --strip-components=1 then remove it (no-op when none present)",
		Action: func(_ context.Context, _ *cli.Command) error {
			return appcontainer.ExtractNPMTarball(os.Stdout, appcontainer.ExtractNPMTarballInput{})
		},
	}
}
