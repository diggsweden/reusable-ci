// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

func commitChangelogReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "commit-changelog-release",
		Usage: "SSH-sign a pre-rendered changelog commit, push main without force, create the final release tag once",
		Description: `EXAMPLE:
   reusable-ci version commit-changelog-release --tag v1.2.3 --repository owner/repo`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagTag, Required: true, Sources: cli.EnvVars("RELEASE_TAG", "TAG_NAME"), Usage: "final stable release tag to create (vMAJOR.MINOR.PATCH)"},
			&cli.StringFlag{Name: "repository", Required: true, Sources: cienv.Repository(), Usage: "repository in owner/name form"},
			&cli.StringFlag{Name: flagBranch, Value: "main", Sources: cli.EnvVars("RELEASE_BRANCH", "BRANCH"), Usage: "branch to push the signed changelog commit to"},
			&cli.StringFlag{Name: "changelog", Value: "CHANGELOG.md", Sources: cli.EnvVars("CHANGELOG_PATH"), Usage: "pre-generated changelog file to commit"},
			&cli.StringFlag{Name: "commit-message-file", Value: defaultCommitMessageFile, Sources: cli.EnvVars("COMMIT_MESSAGE_FILE"), Usage: "pre-generated commit message file used with git commit -F"},
			&cli.StringFlag{Name: "author-name", Sources: cli.EnvVars("GIT_USER_NAME", "COMMIT_AUTHOR_NAME"), Usage: "git user.name for the release bump commit (required; no org default)"},
			&cli.StringFlag{Name: "author-email", Sources: cli.EnvVars("GIT_USER_EMAIL", "COMMIT_AUTHOR_EMAIL"), Usage: "git user.email for the release bump commit (required; no org default)"},
			&cli.StringFlag{Name: "private-key-file", Usage: "path to the OpenSSH private signing key (use '-' for stdin; defaults to $SSH_SIGNING_KEY)"},
			&cli.StringFlag{Name: "host", Sources: cli.EnvVars("RELEASE_GIT_HOST"), Usage: "SSH host for origin and known_hosts pinning (required; no org default)"},
			&cli.StringFlag{Name: "host-key-type", Value: "ed25519", Sources: cli.EnvVars("RELEASE_GIT_HOST_KEY_TYPE"), Usage: "host key type passed to ssh-keyscan"},
			&cli.StringFlag{Name: "host-key-fingerprint", Sources: cli.EnvVars("RELEASE_GIT_HOST_KEY_FINGERPRINT"), Usage: "expected SSH host key fingerprint, required (a trust anchor, never defaulted; get it with: ssh-keyscan -t <type> <host> | ssh-keygen -lf -)"},
			&cli.BoolFlag{Name: "no-sign", Usage: "skip final tag signing (intended for tests; production always signs)"},
			&cli.BoolFlag{Name: "signed", Value: true, Sources: cli.EnvVars("TAG_RELEASE_SIGNED"), Usage: "create a signed final tag; set TAG_RELEASE_SIGNED=false for unsigned annotated test tags"},
			&cli.StringFlag{Name: flagToken, Sources: cienv.ReleaseToken(), Usage: "optional token for HTTP remotes; the Forgejo release flow uses the SSH key instead"},
			dryrun.Flag("git mutations (changelog commit, push, release tag, checkout)"),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				repo := git.New()
				in := appversion.ChangelogReleaseInput{
					Tag:               cmd.String(flagTag),
					Repository:        cmd.String("repository"),
					RemoteHost:        cmd.String("host"),
					Branch:            cmd.String(flagBranch),
					ChangelogPath:     cmd.String("changelog"),
					CommitMessageFile: cmd.String("commit-message-file"),
					AuthorName:        cmd.String("author-name"),
					AuthorEmail:       cmd.String("author-email"),
					TagSigned:         cmd.Bool("signed") && !cmd.Bool("no-sign"),
					Token:             cmd.String(flagToken),
					DryRun:            dryrun.Enabled(cmd),
				}

				if err := appversion.ChangelogReleasePreflight(in); err != nil {
					return err
				}

				if in.DryRun {
					// The SSH signing key, pinned host key, and GIT_SSH_COMMAND
					// only serve the commit/push that dry-run skips — set none
					// of it up (and leave the commit-message files in place for
					// the real run).
					_, _ = fmt.Fprintln(os.Stderr, "[dry-run] skipping SSH signing key and host-key setup (commit and push are skipped)")

					_, err := appversion.ChangelogRelease(ctx, repo, d.OutputSink, os.Stderr, in)

					return err
				}

				privateKey, err := secret.Resolve(cmd.String("private-key-file"), "SSH_SIGNING_KEY")
				if err != nil {
					return err
				}

				if privateKey == "" {
					return errs.CredentialRequired(errs.Credential{What: "SSH signing key", Flag: "private-key-file", Env: "SSH_SIGNING_KEY"})
				}

				_ = os.Unsetenv("SSH_SIGNING_KEY")

				// The host key fingerprint is a trust anchor: the engine ships
				// no default, so require it explicitly before pinning the host.
				fingerprint := cmd.String("host-key-fingerprint")
				if fingerprint == "" {
					return fmt.Errorf("commit-changelog: --host-key-fingerprint is required (a trust anchor; get it with: ssh-keyscan -t %s %s | ssh-keygen -lf -): %w",
						cmd.String("host-key-type"), cmd.String("host"), errs.ErrUsage)
				}

				sshCommand, keyPath, cleanup, err := setupChangelogReleaseSSH(ctx, privateKey, cmd.String("host"), cmd.String("host-key-type"), fingerprint)
				if err != nil {
					return err
				}
				defer cleanup()
				defer cleanupCommitMessageFiles(cmd.String("commit-message-file"))

				restoreSSHCommand := setEnvTemporarily("GIT_SSH_COMMAND", sshCommand)
				defer restoreSSHCommand()

				in.SigningKeyPath = keyPath
				_, err = appversion.ChangelogRelease(ctx, repo, d.OutputSink, os.Stderr, in)

				return err
			})
		},
	}
}

// setupChangelogReleaseSSH prepares an isolated SSH setup (signing key,
// pinned known_hosts, ssh config) in a private temp dir and returns the
// GIT_SSH_COMMAND value, the signing-key path and a cleanup func.
func setupChangelogReleaseSSH(ctx context.Context, privateKey, host, keyType, expectedFingerprint string) (string, string, func(), error) {
	base := os.Getenv("RUNNER_TEMP")
	if base == "" {
		base = os.TempDir()
	}

	dir, err := os.MkdirTemp(base, "forgejo-ci-ssh.")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("commit-changelog: create ssh temp dir: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(dir) } //nolint:gosec // G703 false positive: dir comes from os.MkdirTemp above.

	keyPath := filepath.Join(dir, "signing_key")
	if writeErr := os.WriteFile(keyPath, []byte(ensureTrailingNewline(privateKey)), 0o600); writeErr != nil { //nolint:gosec // G703 false positive: keyPath lives under the os.MkdirTemp dir above.
		cleanup()

		return "", "", func() {}, fmt.Errorf("commit-changelog: write SSH signing key: %w", writeErr)
	}

	pub, err := runTool(ctx, "ssh-keygen", "-y", "-f", keyPath)
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf("commit-changelog: validate SSH signing key: %w", err)
	}

	if writeErr := os.WriteFile(filepath.Join(dir, "signing_key.pub"), []byte(ensureTrailingNewline(pub)), 0o644); writeErr != nil { //nolint:gosec // 0o644 is deliberate for the non-secret public key; the path lives under the os.MkdirTemp dir above.
		cleanup()

		return "", "", func() {}, fmt.Errorf("commit-changelog: write SSH public key: %w", writeErr)
	}

	knownHostsPath, err := pinChangelogReleaseHostKey(ctx, dir, host, keyType, expectedFingerprint)
	if err != nil {
		cleanup()

		return "", "", func() {}, err
	}

	configPath := filepath.Join(dir, "config")

	config := fmt.Sprintf("Host %s\n  IdentityFile %s\n  IdentitiesOnly yes\n  UserKnownHostsFile %s\n  StrictHostKeyChecking yes\n", host, keyPath, knownHostsPath)
	if writeErr := os.WriteFile(configPath, []byte(config), 0o600); writeErr != nil { //nolint:gosec // G703 false positive: configPath lives under the os.MkdirTemp dir above.
		cleanup()

		return "", "", func() {}, fmt.Errorf("commit-changelog: write SSH config: %w", writeErr)
	}

	return "ssh -F " + configPath, keyPath, cleanup, nil
}

// pinChangelogReleaseHostKey scans the SSH host key, verifies its fingerprint
// against the expected value and writes the pinned known_hosts file inside
// dir, returning the final known_hosts path.
func pinChangelogReleaseHostKey(ctx context.Context, dir, host, keyType, expectedFingerprint string) (string, error) {
	knownHostsTmp := filepath.Join(dir, "known_hosts.tmp")

	knownHosts, err := runTool(ctx, "ssh-keyscan", "-t", keyType, host)
	if err != nil {
		return "", fmt.Errorf("commit-changelog: scan SSH host key: %w", err)
	}

	if writeErr := os.WriteFile(knownHostsTmp, []byte(ensureTrailingNewline(knownHosts)), 0o644); writeErr != nil { //nolint:gosec // 0o644 is deliberate for the non-secret known_hosts; the path lives under the caller's os.MkdirTemp dir.
		return "", fmt.Errorf("commit-changelog: write known_hosts: %w", writeErr)
	}

	fingerprintLine, err := runTool(ctx, "ssh-keygen", "-lf", knownHostsTmp)
	if err != nil {
		return "", fmt.Errorf("commit-changelog: fingerprint SSH host key: %w", err)
	}

	if actual := secondField(fingerprintLine); actual != expectedFingerprint {
		return "", fmt.Errorf("commit-changelog: %s SSH host key fingerprint mismatch (got %s, want %s): %w", host, actual, expectedFingerprint, errs.ErrValidation)
	}

	knownHostsPath := filepath.Join(dir, "known_hosts")
	if renameErr := os.Rename(knownHostsTmp, knownHostsPath); renameErr != nil { //nolint:gosec // G703 false positive: both paths live under the caller's os.MkdirTemp dir.
		return "", fmt.Errorf("commit-changelog: finalize known_hosts: %w", renameErr)
	}

	return knownHostsPath, nil
}

func runTool(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := safeexec.Command(ctx, bin, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, safeexec.FirstArg(args))
		if len(out) == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func setEnvTemporarily(name, value string) func() {
	old, hadOld := os.LookupEnv(name)
	_ = os.Setenv(name, value)

	return func() {
		if hadOld {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	}
}

func cleanupCommitMessageFiles(commitMessageFile string) {
	_ = os.Remove("commit-body.txt")
	if commitMessageFile == "" || commitMessageFile == defaultCommitMessageFile {
		_ = os.Remove(defaultCommitMessageFile)

		return
	}

	_ = os.Remove(commitMessageFile)
}

func secondField(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}

	return fields[1]
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}

	return value + "\n"
}
