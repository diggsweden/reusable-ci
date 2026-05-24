// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func googlePlayCmd() *cli.Command {
	return &cli.Command{
		Name:  "google-play",
		Usage: "Google Play publish helpers",
		Commands: []*cli.Command{
			googlePlayFindAABCmd(),
			googlePlayValidateCredentialsCmd(),
		},
	}
}

func googlePlayValidateCredentialsCmd() *cli.Command {
	const envName = "GOOGLE_PLAY_SERVICE_ACCOUNT_JSON"

	return &cli.Command{
		Name:  "validate-credentials",
		Usage: "validate GOOGLE_PLAY_SERVICE_ACCOUNT_JSON is present and looks like a Google service-account key",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			value, err := secret.Resolve("", envName)
			if err != nil {
				return err
			}

			if value == "" {
				annot.Errorf("%s secret not found", envName)
				annot.Errorf("Please add a service account JSON key with Google Play publishing permissions")

				return fmt.Errorf("%s is required: %w", envName, errs.ErrMissingInput)
			}

			return apppublish.GooglePlayCheckCredentials(ctx, os.Stderr, apppublish.GooglePlayCheckCredentialsInput{
				ServiceAccountJSON: value,
			})
		},
	}
}

func googlePlayFindAABCmd() *cli.Command {
	return &cli.Command{
		Name:  "find-aab",
		Usage: "find an AAB artifact and emit aab-file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Value: "artifacts", Sources: cli.EnvVars("ARTIFACTS_DIR"), Usage: "directory scanned for an .aab artifact"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)
				_, err := apppublish.FindArtifact(ctx, d.OutputSink, os.Stderr, annot, apppublish.FindArtifactInput{
					Dir:       cmd.String("dir"),
					Ext:       ".aab",
					OutputKey: "aab-file",
					Label:     "AAB",
					Recursive: true,
				})

				return err
			})
		},
	}
}
