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
		Description: `EXAMPLE:
   # Key ID from $APP_STORE_CONNECT_API_KEY_ID; base64 key from env or --private-key-file
   reusable-ci publish appstore prepare-credentials --output-dir private_keys`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output-dir", Value: "private_keys", Sources: cli.EnvVars("PRIVATE_KEYS_DIR"), Usage: "destination directory for the decoded key"},
			&cli.StringFlag{Name: "private-key-file", Usage: "path to a file containing the base64 App Store Connect API private key (use \"-\" for stdin; defaults to $APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keyID, err := secret.Resolve("", keyIDEnv)
			if err != nil {
				return err
			}

			if keyID == "" {
				return errs.CredentialRequired(errs.Credential{What: "App Store Connect API key ID", Env: keyIDEnv})
			}

			b64, err := secret.Resolve(cmd.String("private-key-file"), b64Env)
			if err != nil {
				return err
			}

			if b64 == "" {
				return errs.CredentialRequired(errs.Credential{What: "App Store Connect API private key", Flag: "private-key-file", Env: b64Env})
			}

			return apppublish.PrepareAppStoreCredentials(ctx, os.Stderr, apppublish.AppStorePrepareCredentialsInput{
				Dir:           cmd.String("output-dir"),
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
		Description: `EXAMPLE:
   reusable-ci publish appstore find-ipa --artifact-dir artifacts`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "artifact-dir", Value: "artifacts", Sources: cli.EnvVars("ARTIFACT_DIR"), Usage: "directory scanned for an .ipa artifact"}, //nolint:goconst // shared flag name across artifact-consuming commands.
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)
				_, err := apppublish.FindArtifact(ctx, d.OutputSink, os.Stderr, annot, apppublish.FindArtifactInput{
					Dir:       cmd.String("artifact-dir"),
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
		Description: `EXAMPLE:
   reusable-ci publish appstore parse-upload-result --path upload-result.json`,
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
