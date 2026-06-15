// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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

func appStoreCmd() *cli.Command {
	return &cli.Command{
		Name:  "appstore",
		Usage: "Apple App Store publish helpers",
		Commands: []*cli.Command{
			appStoreFindIPACmd(),
			appStoreParseUploadResultCmd(),
			appStorePrepareCredentialsCmd(),
		},
	}
}

func appStorePrepareCredentialsCmd() *cli.Command {
	const (
		keyIDEnv = "APP_STORE_CONNECT_API_KEY_ID"
		b64Env   = "APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64"
	)

	return &cli.Command{
		Name:  "prepare-credentials",
		Usage: "decode the App Store Connect API private key to private_keys/AuthKey_<KEY_ID>.p8 (mode 0600)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out-dir", Value: "private_keys", Usage: "destination directory for the decoded key"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)

			keyID, err := secret.Resolve("", keyIDEnv)
			if err != nil {
				return err
			}

			if keyID == "" {
				annot.Errorf("%s secret not found", keyIDEnv)

				return fmt.Errorf("%s is required: %w", keyIDEnv, errs.ErrMissingInput)
			}

			b64, err := secret.Resolve("", b64Env)
			if err != nil {
				return err
			}

			if b64 == "" {
				annot.Errorf("%s secret not found", b64Env)

				return fmt.Errorf("%s is required: %w", b64Env, errs.ErrMissingInput)
			}

			return apppublish.PrepareAppStoreCredentials(ctx, os.Stderr, apppublish.AppStorePrepareCredentialsInput{
				Dir:           cmd.String("out-dir"),
				KeyID:         keyID,
				PrivateKeyB64: b64,
			})
		},
	}
}

func appStoreFindIPACmd() *cli.Command {
	return &cli.Command{
		Name:  "find-ipa",
		Usage: "find an IPA artifact and emit ipa-file",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifacts-dir", Value: "artifacts", Sources: cli.EnvVars("ARTIFACTS_DIR"), Usage: "directory scanned for an .ipa artifact"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)
				_, err := apppublish.FindArtifact(ctx, d.OutputSink, os.Stderr, annot, apppublish.FindArtifactInput{
					Dir:       cmd.String("artifacts-dir"),
					Ext:       ".ipa",
					OutputKey: "ipa-file",
					Label:     "IPA",
					Recursive: true,
				})

				return err
			})
		},
	}
}

func appStoreParseUploadResultCmd() *cli.Command {
	return &cli.Command{
		Name:  "parse-upload-result",
		Usage: "parse altool upload-result JSON and emit request-id",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "path", Value: "upload-result.json", Sources: cli.EnvVars("UPLOAD_RESULT"), Usage: "path to the altool upload-result JSON to parse"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return apppublish.EmitAppStoreUploadResult(ctx, d.OutputSink, os.Stderr, apppublish.AppStoreUploadResultInput{Path: cmd.String("path")})
			})
		},
	}
}
