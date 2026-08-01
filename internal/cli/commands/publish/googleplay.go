// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
		Description: `EXAMPLE:
   # Credential from env, or --credentials-file ("-" for stdin)
   reusable-ci publish google-play validate-credentials`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "credentials-file", Usage: "path to a file containing the Google Play service-account JSON (use \"-\" for stdin; defaults to $GOOGLE_PLAY_SERVICE_ACCOUNT_JSON)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			value, err := secret.Resolve(cmd.String("credentials-file"), envName)
			if err != nil {
				return err
			}

			if value == "" {
				annot.Errorf("add a service account JSON key with Google Play publishing permissions")

				return errs.CredentialRequired(errs.Credential{What: "Google Play service-account key", Flag: "credentials-file", Env: envName})
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
		Description: `EXAMPLE:
   reusable-ci publish google-play find-aab --artifact-dir artifacts`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-dir", Value: "artifacts", Sources: cli.EnvVars("ARTIFACT_DIR"), Usage: "directory scanned for an .aab artifact"}, //nolint:goconst // shared flag name across artifact-consuming commands.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)
				_, err := apppublish.FindArtifact(ctx, d.OutputSink, os.Stderr, annot, apppublish.FindArtifactInput{
					Dir:       cmd.String("artifact-dir"),
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
