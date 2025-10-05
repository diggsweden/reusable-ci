// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
)

func extractNPMTarballCmd() *cli.Command {
	return &cli.Command{
		Name:  "extract-npm-tarball",
		Usage: "extract a top-level *.tgz / *.tar.gz with --strip-components=1 then remove it (no-op when none present)",
		Description: `EXAMPLE:
   # Run in the directory holding the packed tarball (takes no flags)
   reusable-ci container extract-npm-tarball`,
		Action: func(_ context.Context, _ *cli.Command) error {
			return appcontainer.ExtractNPMTarball(os.Stderr, appcontainer.ExtractNPMTarballInput{})
		},
	}
}
