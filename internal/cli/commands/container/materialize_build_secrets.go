// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func materializeBuildSecretsCmd() *cli.Command {
	return &cli.Command{
		Name:  "materialize-build-secrets",
		Usage: "unpack REUSABLE_CI_BUILD_SECRETS_JSON into mode-0600 tmpfiles and emit secret-mounts",
		Description: "Reads the JSON envelope of build-secret values forwarded by the caller " +
			"workflow, writes each declared name to a per-secret tmpfile at mode 0600 under " +
			"$RUNNER_TEMP/build-secrets, and emits a `secret-mounts` output (id=NAME,src=PATH " +
			"lines) for `container build --secrets`. Used by publish-container.yml; " +
			"never invoke directly from an adopter workflow.\n\n" +
			"EXAMPLE:\n" +
			"   BUILD_SECRET_NAMES=\"DB_PASSWORD\" \\\n" +
			"   REUSABLE_CI_BUILD_SECRETS_JSON='{\"DB_PASSWORD\":\"…\"}' \\\n" +
			"   reusable-ci container materialize-build-secrets",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "names",
				Sources: cli.EnvVars("BUILD_SECRET_NAMES"),
				Usage:   "newline/comma/space-separated list of build-secret names from containers[].build-secrets",
			},
			&cli.StringFlag{
				Name:    "envelope",
				Sources: cli.EnvVars("REUSABLE_CI_BUILD_SECRETS_JSON"),
				Usage:   "JSON object mapping each declared name to its value; sourced from the workflow secret of the same name",
			},
			&cli.StringFlag{
				Name:    "output-dir",
				Sources: cli.EnvVars("BUILD_SECRETS_DIR"),
				Usage:   "directory for the materialized tmpfiles; defaults to <temp-dir>/build-secrets",
			},
			&cli.StringFlag{
				Name:    flagTempDir,
				Sources: cienv.TempDir(),
				Usage:   "scratch directory the default --output-dir is sited under",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appcontainer.MaterializeBuildSecrets(ctx, d.OutputSink, os.Stderr, appcontainer.MaterializeBuildSecretsInput{
					Names:        cmd.String("names"),
					EnvelopeJSON: cmd.String("envelope"),
					OutputDir:    cmd.String("output-dir"),
					TempDir:      cmd.String(flagTempDir),
				})
			})
		},
	}
}
