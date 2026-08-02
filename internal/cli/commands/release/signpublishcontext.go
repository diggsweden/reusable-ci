// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
)

func signPublishContextCmd() *cli.Command {
	return &cli.Command{
		Name:  "sign-publish-context",
		Usage: "export the sign-and-publish workflow env contract",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagReleaseSHA, Required: true, Sources: cli.EnvVars("RELEASE_SHA"), Usage: "prepared release commit SHA"},
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cienv.Tag(), Usage: "stable release tag"},
			&cli.StringFlag{Name: "artifact-path", Value: defaultDistPath, Sources: cli.EnvVars("ARTIFACT_PATH"), Usage: "artifact extraction path whose normalized value becomes DIST_DIR"},
			&cli.StringFlag{Name: "env-file", Required: true, Sources: cli.EnvVars("FORGEJO_ENV", "GITHUB_ENV"), Usage: "runner env file receiving RELEASE_*, DIST_DIR, and state-dir entries"},
			&cli.StringFlag{Name: "runner-temp", Sources: cienv.TempDir(), Usage: "runner temp directory for the sign-and-publish state dir"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return apprelease.SignPublishContext(os.Stderr, apprelease.SignPublishContextInput{
				ReleaseSHA:   cmd.String(flagReleaseSHA),
				ReleaseTag:   cmd.String(flagTag),
				ArtifactPath: cmd.String("artifact-path"),
				EnvFile:      cmd.String("env-file"),
				RunnerTemp:   cmd.String("runner-temp"),
			})
		},
	}
}
