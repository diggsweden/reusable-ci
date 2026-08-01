// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
)

// secretGroup wires `reusable-ci validate secret <kind>` — pre-flight
// checks that required signing/publishing secrets are present in the
// environment before a release stage tries to use them.
func secretGroup() *cli.Command {
	return &cli.Command{
		Name:  "secret",
		Usage: "validate that a release-related secret is present in the environment",
		Commands: []*cli.Command{
			secretGPGPublicKeyCmd(),
			secretMavenCentralCmd(),
		},
	}
}

func secretGPGPublicKeyCmd() *cli.Command {
	return &cli.Command{
		Name:  "gpg-public-key",
		Usage: "fail when RELEASE_GPG_PUBLIC_KEY is unset",
		Description: `EXAMPLE:
   # Reads $RELEASE_GPG_PUBLIC_KEY
   reusable-ci validate secret gpg-public-key`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "release-gpg-public-key",
				Sources: cli.EnvVars("RELEASE_GPG_PUBLIC_KEY"),
				Usage:   "armored GPG public key whose presence is asserted (defaults to $RELEASE_GPG_PUBLIC_KEY)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.GPGPublicKey(os.Stderr, cmd.String("release-gpg-public-key"))
		},
	}
}

func secretMavenCentralCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-central",
		Usage: "verify $MAVEN_CENTRAL_USERNAME and $MAVEN_CENTRAL_PASSWORD are set",
		Description: `EXAMPLE:
   # Reads $MAVEN_CENTRAL_USERNAME / $MAVEN_CENTRAL_PASSWORD
   reusable-ci validate secret maven-central`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "username",
				Sources: cli.EnvVars("MAVEN_CENTRAL_USERNAME"),
				Usage:   "Maven Central account username (defaults to $MAVEN_CENTRAL_USERNAME)",
			},
			&cli.StringFlag{
				Name:  "password-file",
				Usage: "path to a file containing the Maven Central password (use \"-\" for stdin; defaults to $MAVEN_CENTRAL_PASSWORD)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			password, err := secret.Resolve(cmd.String("password-file"), "MAVEN_CENTRAL_PASSWORD")
			if err != nil {
				return err
			}

			annot := deps.Annotator(cmd)

			return appvalidate.MavenCentralCredentials(os.Stderr, os.Stderr, annot, appvalidate.MavenCentralCredentialsInput{
				Username: cmd.String("username"),
				Password: password,
			})
		},
	}
}
