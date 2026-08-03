// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
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
			gpgSignPackagesCmd(),
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
				Name:  flagPassphraseFile,
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
				return errs.CredentialRequired(errs.Credential{What: credentialGPGPrivateKey, Flag: flagPrivateKeyFile, Env: "GPG_PRIVATE_KEY"})
			}

			passphrase, err := secret.Resolve(cmd.String(flagPassphraseFile), "GPG_PASSPHRASE")
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

func gpgSignPackagesCmd() *cli.Command {
	return &cli.Command{
		Name:  "sign-packages",
		Usage: "GPG detach-sign .deb/.rpm/.apk packages with binary .sig sidecars",
		Description: `Imports the supplied GPG private key into an ephemeral GNUPGHOME,
verifies the imported key fingerprint, signs distro packages under --dir, and
then removes the temporary keyring. The private key is passed to gpg via stdin;
the passphrase is passed via gpg --passphrase-fd 0.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagPrivateKeyFile, Usage: "path to armored GPG private key (use \"-\" for stdin; defaults to $GPG_PRIVATE_KEY or $GPG_SIGNING_KEY)"},
			&cli.StringFlag{Name: flagPassphraseFile, Usage: "path to GPG passphrase (use \"-\" for stdin; defaults to $GPG_PASSPHRASE or $GPG_SIGNING_PASSWORD)"},
			&cli.StringFlag{Name: "fingerprint", Sources: cli.EnvVars("GPG_FINGERPRINT", "GPG_SIGNING_FINGERPRINT"), Usage: "expected imported key fingerprint (required)"},
			&cli.StringFlag{Name: "dir", Value: defaultDistDir, Usage: "directory containing .deb/.rpm/.apk packages"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			privateKey, err := resolveSecretAny(cmd.String(flagPrivateKeyFile), "GPG_PRIVATE_KEY", "GPG_SIGNING_KEY")
			if err != nil {
				return err
			}

			if privateKey == "" {
				return errs.CredentialRequired(errs.Credential{What: credentialGPGPrivateKey, Flag: flagPrivateKeyFile, Env: "GPG_PRIVATE_KEY or GPG_SIGNING_KEY"})
			}

			passphrase, err := resolveSecretAny(cmd.String(flagPassphraseFile), "GPG_PASSPHRASE", "GPG_SIGNING_PASSWORD")
			if err != nil {
				return err
			}

			if passphrase == "" {
				return errs.CredentialRequired(errs.Credential{What: "GPG passphrase", Flag: flagPassphraseFile, Env: "GPG_PASSPHRASE or GPG_SIGNING_PASSWORD"})
			}

			fingerprint := cmd.String("fingerprint")

			return withEphemeralGNUPGHome(func() error {
				_, err := apprelease.GPGSignPackages(ctx, gpg.NewIsolated(), os.Stderr, apprelease.GPGSignPackagesInput{
					PrivateKey:  privateKey,
					Fingerprint: fingerprint,
					Passphrase:  passphrase,
					Dir:         cmd.String("dir"),
				})

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

func resolveSecretAny(filePath string, envVars ...string) (string, error) {
	if filePath != "" {
		return secret.Resolve(filePath, "")
	}

	for _, envVar := range envVars {
		value, err := secret.Resolve("", envVar)
		if err != nil || value != "" {
			return value, err
		}
	}

	return "", nil
}

func withEphemeralGNUPGHome(fn func() error) error {
	base := runcontext.TempDir().Resolve(os.Getenv)
	if base == "" {
		base = os.TempDir()
	}

	if info, err := os.Stat(base); err != nil || !info.IsDir() { //nolint:gosec // G703 false positive: RUNNER_TEMP is trusted CI runner configuration, not request input.
		base = os.TempDir()
	}

	home, err := os.MkdirTemp(base, "reusable-ci-gnupg-*")
	if err != nil {
		return fmt.Errorf("create temporary GNUPGHOME under %s: %w", filepath.Clean(base), err)
	}

	if err := os.Chmod(home, 0o700); err != nil { //nolint:gosec // G302 false positive: 0o700 is the intentionally private mode gpg requires for GNUPGHOME.
		_ = os.RemoveAll(home) //nolint:gosec // G703 false positive: home comes from os.MkdirTemp above.

		return fmt.Errorf("chmod temporary GNUPGHOME: %w", err)
	}

	old, had := os.LookupEnv("GNUPGHOME")

	if err := os.Setenv("GNUPGHOME", home); err != nil {
		_ = os.RemoveAll(home) //nolint:gosec // G703 false positive: home comes from os.MkdirTemp above.

		return fmt.Errorf("set GNUPGHOME: %w", err)
	}

	defer func() {
		if had {
			_ = os.Setenv("GNUPGHOME", old)
		} else {
			_ = os.Unsetenv("GNUPGHOME")
		}

		_ = os.RemoveAll(home) //nolint:gosec // G703 false positive: home comes from os.MkdirTemp above.
	}()

	return fn()
}
