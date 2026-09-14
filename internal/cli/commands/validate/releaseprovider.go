// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func releaseProviderCmd() *cli.Command {
	return &cli.Command{
		Name:  "release-provider",
		Usage: "refuse release workflows on provider instances their pinned actions do not support",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "server-url", Sources: cienv.ServerURL(), Usage: "forge server URL supplied by the runner"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appvalidate.ReleaseProvider(d.Platform, cmd.String("server-url"))
			})
		},
	}
}
