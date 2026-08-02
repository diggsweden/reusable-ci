// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// gpgGroup wires `reusable-ci release gpg <verb>` — manages the GPG
// key lifecycle for the release flow: import a private key (with
// optional gpg-agent passphrase pre-seeding + git-signing config),
// then clean it up post-release.
func gpgGroup() *cli.Command {
	return &cli.Command{
		Name:  "gpg",
		Usage: "manage the release GPG key (import / cleanup)",
		Commands: []*cli.Command{
			gpgImportCmd(),
			gpgCleanupCmd(),
		},
	}
}

func gpgImportCmd() *cli.Command {
	return &cli.Command{
		Name:  "import",
		Usage: "import a GPG private key, optionally cache the passphrase, optionally configure git signing",
		Description: `EXAMPLE:
   # Key + passphrase from files (or stdin via "-"); defaults to $GPG_PRIVATE_KEY / $GPG_PASSPHRASE
   reusable-ci release gpg import --private-key-file key.asc --git-user-signingkey --git-commit-gpgsign`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  flagPrivateKeyFile,
				Usage: "path to a file containing the armored GPG private key (use \"-\" for stdin; defaults to $GPG_PRIVATE_KEY)",
			},
			&cli.StringFlag{
				Name:  "passphrase-file",
				Usage: "path to a file containing the GPG passphrase (use \"-\" for stdin; defaults to $GPG_PASSPHRASE)",
			},
			&cli.BoolFlag{Name: "git-user-signingkey", Sources: cli.EnvVars("GIT_USER_SIGNINGKEY"),
				Usage: "write user.signingkey/name/email from the imported key"},
			&cli.BoolFlag{Name: "git-commit-gpgsign", Sources: cli.EnvVars("GIT_COMMIT_GPGSIGN"),
				Usage: "additionally write commit.gpgsign=true"},
			&cli.BoolFlag{Name: "git-config-global", Sources: cli.EnvVars("REUSABLE_CI_GIT_CONFIG_GLOBAL"),
				Usage: "use --global on the git config writes (env $REUSABLE_CI_GIT_CONFIG_GLOBAL — NOT git's reserved $GIT_CONFIG_GLOBAL, which is a path)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			privateKey, err := secret.Resolve(cmd.String(flagPrivateKeyFile), "GPG_PRIVATE_KEY")
			if err != nil {
				return err
			}

			if privateKey == "" {
				return errs.CredentialRequired(errs.Credential{What: "GPG private key", Flag: flagPrivateKeyFile, Env: "GPG_PRIVATE_KEY"})
			}

			passphrase, err := secret.Resolve(cmd.String("passphrase-file"), "GPG_PASSPHRASE")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				_, err := apprelease.GPGImport(ctx, gpg.New(), openpgp.ReadMetadata, git.New(), d.OutputSink,
					apprelease.GPGImportInput{
						PrivateKey:        privateKey,
						Passphrase:        passphrase,
						GitUserSigningKey: cmd.Bool("git-user-signingkey"),
						GitCommitGPGSign:  cmd.Bool("git-commit-gpgsign"),
						GitConfigGlobal:   cmd.Bool("git-config-global"),
					},
					os.Stderr)

				return err
			})
		},
	}
}

func gpgCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  "cleanup",
		Usage: "delete the imported GPG key and stop gpg-agent (idempotent; safe under if: always())",
		Description: `EXAMPLE:
   reusable-ci release gpg cleanup --fingerprint 1234ABCD...`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "fingerprint", Sources: cli.EnvVars("GPG_FINGERPRINT"), Usage: "GPG key fingerprint to delete from the local keyring"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			apprelease.GPGCleanup(ctx, gpg.New(), cmd.String("fingerprint"), os.Stderr)

			return nil
		},
	}
}
