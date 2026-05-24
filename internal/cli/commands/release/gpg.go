// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "private-key-file",
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
			&cli.BoolFlag{Name: "git-config-global", Sources: cli.EnvVars("GIT_CONFIG_GLOBAL"),
				Usage: "use --global on the git config writes"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			privateKey, err := secret.Resolve(cmd.String("private-key-file"), "GPG_PRIVATE_KEY")
			if err != nil {
				return err
			}

			if privateKey == "" {
				return fmt.Errorf("the GPG private key is required: pass --private-key-file <path> (\"-\" for stdin) or set $GPG_PRIVATE_KEY: %w", errs.ErrUsage)
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
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "fingerprint", Sources: cli.EnvVars("GPG_FINGERPRINT"), Usage: "GPG key fingerprint to delete from the local keyring"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			apprelease.GPGCleanup(ctx, gpg.New(), cmd.String("fingerprint"), os.Stderr)

			return nil
		},
	}
}
