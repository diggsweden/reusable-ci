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
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// sshGroup wires `reusable-ci release ssh <verb>` — the SSH sibling of the
// gpg group. setup configures git to sign commits/tags with an SSH key (no
// long-lived OpenPGP key on the runner); cleanup removes the key file.
//
// Both verbs share the same on-disk key path so cleanup can run unconditionally
// under `if: always()`, exactly like `release gpg cleanup`.
func sshGroup() *cli.Command {
	return &cli.Command{
		Name:  "ssh",
		Usage: "configure SSH-based git commit/tag signing (setup / cleanup)",
		Commands: []*cli.Command{
			sshSetupCmd(),
			sshCleanupCmd(),
		},
	}
}

func sshSetupCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup",
		Usage: "write the SSH signing key and configure git (gpg.format=ssh, user.signingkey)",
		Description: `EXAMPLE:
   # Key from a file (or stdin via "-"); defaults to $SSH_SIGNING_KEY
   reusable-ci release ssh setup --private-key-file id_ed25519 --git-commit-sign`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "private-key-file",
				Usage: "path to the OpenSSH private signing key (use \"-\" for stdin; defaults to $SSH_SIGNING_KEY)",
			},
			&cli.StringFlag{
				Name:    "key-path",
				Sources: cli.EnvVars("SSH_SIGNING_KEY_PATH"),
				Usage:   "where to write the signing key (default: $RUNNER_TEMP/reusable-ci-ssh-signing-key)",
			},
			&cli.StringFlag{Name: "author-name", Sources: cli.EnvVars("COMMIT_AUTHOR_NAME"),
				Usage: "write user.name"},
			&cli.StringFlag{Name: "author-email", Sources: cli.EnvVars("COMMIT_AUTHOR_EMAIL"),
				Usage: "write user.email"},
			&cli.BoolFlag{Name: "git-commit-sign", Sources: cli.EnvVars("GIT_COMMIT_GPGSIGN"),
				Usage: "additionally write commit.gpgsign=true"},
			&cli.BoolFlag{Name: "git-config-global", Sources: cli.EnvVars("GIT_CONFIG_GLOBAL"),
				Usage: "use --global on the git config writes"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			privateKey, err := secret.Resolve(cmd.String("private-key-file"), "SSH_SIGNING_KEY")
			if err != nil {
				return err
			}

			if privateKey == "" {
				return errs.CredentialRequired(errs.Credential{What: "SSH signing key", Flag: "private-key-file", Env: "SSH_SIGNING_KEY"})
			}

			keyPath := resolveSSHKeyPath(cmd.String("key-path"))

			writeKey := func(key []byte) (string, error) {
				if mkErr := os.MkdirAll(filepath.Dir(keyPath), 0o700); mkErr != nil {
					return "", fmt.Errorf("mkdir for ssh signing key: %w", mkErr)
				}

				if wErr := os.WriteFile(keyPath, key, 0o600); wErr != nil {
					return "", fmt.Errorf("write ssh signing key: %w", wErr)
				}

				return keyPath, nil
			}

			return apprelease.SSHSigningSetup(ctx, git.New(), writeKey, apprelease.SSHSigningInput{
				PrivateKey:      privateKey,
				AuthorName:      cmd.String("author-name"),
				AuthorEmail:     cmd.String("author-email"),
				GitCommitSign:   cmd.Bool("git-commit-sign"),
				GitConfigGlobal: cmd.Bool("git-config-global"),
			}, os.Stderr)
		},
	}
}

func sshCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  "cleanup",
		Usage: "remove the SSH signing key file (idempotent; safe under if: always())",
		Description: `EXAMPLE:
   reusable-ci release ssh cleanup`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "key-path",
				Sources: cli.EnvVars("SSH_SIGNING_KEY_PATH"),
				Usage:   "key file to remove (default: $RUNNER_TEMP/reusable-ci-ssh-signing-key)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			keyPath := resolveSSHKeyPath(cmd.String("key-path"))

			// Idempotent: a missing key (setup never ran, or already cleaned)
			// is success, so this is safe under if: always().
			if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove ssh signing key: %w", err)
			}

			return nil
		},
	}
}

// resolveSSHKeyPath returns the override when set, otherwise a path under
// $RUNNER_TEMP (the runner's per-job scratch dir, cleaned automatically) and
// falling back to the OS temp dir off a runner. setup and cleanup share it so
// the key written by one is the key removed by the other.
func resolveSSHKeyPath(override string) string {
	if override != "" {
		return override
	}

	base := os.Getenv("RUNNER_TEMP")
	if base == "" {
		base = os.TempDir()
	}

	return filepath.Join(base, "reusable-ci-ssh-signing-key")
}
