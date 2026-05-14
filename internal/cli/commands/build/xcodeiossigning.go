// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/xcode"
	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
)

func xcodeIOSSetupCodeSigningCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-code-signing",
		Usage: "decode base64 cert+profile, create a transient macOS keychain, install the profile",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cert-base64", Sources: cli.EnvVars("IOS_SIGNING_CERTIFICATE_BASE64")},
			&cli.StringFlag{Name: "cert-passphrase", Sources: cli.EnvVars("IOS_SIGNING_CERTIFICATE_PASSPHRASE")},
			&cli.StringFlag{Name: "pp-base64", Sources: cli.EnvVars("PROVISIONING_PROFILE_BASE64")},
			&cli.StringFlag{Name: "keychain-password", Sources: cli.EnvVars("KEYCHAIN_PASSWORD")},
			&cli.StringFlag{Name: "temp-dir", Sources: cli.EnvVars("CI_TEMP_DIR", "RUNNER_TEMP")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appbuild.XcodeSetupCodeSigning(ctx, xcode.NewSecurity(), os.Stdout, appbuild.XcodeSetupCodeSigningInput{
				CertBase64:       cmd.String("cert-base64"),
				CertPassphrase:   cmd.String("cert-passphrase"),
				PPBase64:         cmd.String("pp-base64"),
				KeychainPassword: cmd.String("keychain-password"),
				TempDir:          cmd.String("temp-dir"),
			})
		},
	}
}
