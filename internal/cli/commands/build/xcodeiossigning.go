// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/xcode"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/cli/secret"
)

func xcodeIOSSetupCodeSigningCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-code-signing",
		Usage: "decode base64 cert+profile, create a transient macOS keychain, install the profile",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cert-base64", Sources: cli.EnvVars("IOS_SIGNING_CERTIFICATE_BASE64"), Usage: "base64-encoded P12 signing certificate body"},
			&cli.StringFlag{
				Name:  "cert-passphrase-file",
				Usage: "path to a file containing the signing certificate passphrase (use \"-\" for stdin; defaults to $IOS_SIGNING_CERTIFICATE_PASSPHRASE)",
			},
			&cli.StringFlag{Name: "pp-base64", Sources: cli.EnvVars("PROVISIONING_PROFILE_BASE64"), Usage: "base64-encoded provisioning profile (.mobileprovision)"},
			&cli.StringFlag{
				Name:  "keychain-password-file",
				Usage: "path to a file containing the transient keychain password (use \"-\" for stdin; defaults to $KEYCHAIN_PASSWORD)",
			},
			&cli.StringFlag{Name: "temp-dir", Sources: cli.EnvVars("CI_TEMP_DIR", "RUNNER_TEMP"), Usage: "directory the decoded cert / profile / keychain are written under"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			certPassphrase, err := secret.Resolve(cmd.String("cert-passphrase-file"), "IOS_SIGNING_CERTIFICATE_PASSPHRASE")
			if err != nil {
				return err
			}

			keychainPassword, err := secret.Resolve(cmd.String("keychain-password-file"), "KEYCHAIN_PASSWORD")
			if err != nil {
				return err
			}

			return appbuild.XcodeSetupCodeSigning(ctx, xcode.NewSecurity(), os.Stderr, appbuild.XcodeSetupCodeSigningInput{
				CertBase64:       cmd.String("cert-base64"),
				CertPassphrase:   certPassphrase,
				PPBase64:         cmd.String("pp-base64"),
				KeychainPassword: keychainPassword,
				TempDir:          cmd.String("temp-dir"),
			})
		},
	}
}
