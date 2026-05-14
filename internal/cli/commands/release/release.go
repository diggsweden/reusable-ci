// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package release wires `reusable-ci release <subcmd>` using urfave/cli v3.
package release

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

// New returns the `release` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "release",
		Usage: "release-flow helpers (GPG import/cleanup, signing, checksums, notes, create, …)",
		Commands: []*cli.Command{
			gpgImportCmd(),
			gpgCleanupCmd(),
			signCmd(),
			checksumsCmd(),
			sbomZipCmd(),
			notesCmd(),
			validateChangelogCmd(),
			resolveArtifactNameCmd(),
			resolveMetadataCmd(),
			createCmd(),
		},
	}
}

func gpgImportCmd() *cli.Command {
	return &cli.Command{
		Name:  "gpg-import",
		Usage: "import a GPG private key, optionally cache the passphrase, optionally configure git signing",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "private-key", Required: true, Sources: cli.EnvVars("GPG_PRIVATE_KEY")},
			&cli.StringFlag{Name: "passphrase", Sources: cli.EnvVars("GPG_PASSPHRASE")},
			&cli.BoolFlag{Name: "git-user-signingkey", Sources: cli.EnvVars("GIT_USER_SIGNINGKEY"),
				Usage: "write user.signingkey/name/email from the imported key"},
			&cli.BoolFlag{Name: "git-commit-gpgsign", Sources: cli.EnvVars("GIT_COMMIT_GPGSIGN"),
				Usage: "additionally write commit.gpgsign=true"},
			&cli.BoolFlag{Name: "git-config-global", Sources: cli.EnvVars("GIT_CONFIG_GLOBAL"),
				Usage: "use --global on the git config writes"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = d.Close(ctx) }()

			_, err = apprelease.GPGImport(ctx, gpg.New(), git.New(), d.OutputSink,
				apprelease.GPGImportInput{
					PrivateKey:        cmd.String("private-key"),
					Passphrase:        cmd.String("passphrase"),
					GitUserSigningKey: cmd.Bool("git-user-signingkey"),
					GitCommitGPGSign:  cmd.Bool("git-commit-gpgsign"),
					GitConfigGlobal:   cmd.Bool("git-config-global"),
				},
				os.Stdout)
			return err
		},
	}
}

func gpgCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  "gpg-cleanup",
		Usage: "delete the imported GPG key and stop gpg-agent (idempotent; safe under if: always())",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "fingerprint", Sources: cli.EnvVars("GPG_FINGERPRINT")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			apprelease.GPGCleanup(ctx, gpg.New(), cmd.String("fingerprint"), os.Stdout)
			return nil
		},
	}
}
