// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// SSHKeyWriter persists the OpenSSH private signing key and returns the path
// it was written to. Production writes a 0600 file inside a 0700 directory;
// tests capture the bytes in memory. Keeping the filesystem behind this
// function keeps SSHSigningSetup unit-testable without touching disk.
type SSHKeyWriter func(key []byte) (path string, err error)

// SSHSigningInput drives `reusable-ci release ssh setup`. It is the SSH sibling
// of GPGImportInput: same git-signing knobs, a different on-disk key shape.
type SSHSigningInput struct {
	PrivateKey      string // OpenSSH private key material (required)
	AuthorName      string // optional; written to user.name
	AuthorEmail     string // optional; written to user.email
	GitCommitSign   bool   // also write commit.gpgsign=true (signs `git commit` without -S)
	GitConfigGlobal bool   // use --global on the git config writes
}

// SSHSigningSetup configures git to sign commits and tags with an SSH key. It
// writes the private key to disk via writeKey, then sets gpg.format=ssh and
// user.signingkey to the key path. The downstream `git tag -s` /
// `git commit -S` (version tag-release / commit-push) then sign over SSH
// transparently — exactly as the GPG path leaves the actual signing to git's
// configured backend. No long-lived OpenPGP key is imported onto the runner.
//
// It reuses gitSigningOps (the same Run+Config slice GPGImport configures git
// through), so the two signing backends share one git-config seam.
//
// Cleanup (removing the key file) is the caller's `if: always()` step, paired
// like GPGCleanup is with GPGImport.
func SSHSigningSetup(ctx context.Context, gitr gitSigningOps, writeKey SSHKeyWriter, in SSHSigningInput, out io.Writer) error {
	if err := validateSSHSigningInputs(gitr, writeKey, in); err != nil {
		return err
	}

	keyPath, err := writeKey([]byte(ensureTrailingNewline(in.PrivateKey)))
	if err != nil {
		return fmt.Errorf("ssh-signing: write key: %w", err)
	}

	if err := configureSSHSigning(ctx, gitr, keyPath, in); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "%s Configured git for SSH commit/tag signing (key: %s)\n", clicolor.Check(out), keyPath)

	return nil
}

func validateSSHSigningInputs(gitr gitSigningOps, writeKey SSHKeyWriter, in SSHSigningInput) error {
	switch {
	case gitr == nil:
		return fmt.Errorf("ssh-signing: git ops are required: %w", errs.ErrUsage)
	case writeKey == nil:
		return fmt.Errorf("ssh-signing: key writer is required: %w", errs.ErrUsage)
	case strings.TrimSpace(in.PrivateKey) == "":
		return fmt.Errorf("ssh-signing: PrivateKey is required: %w", errs.ErrUsage)
	}

	return nil
}

// configureSSHSigning writes the git config keys that switch signing to SSH.
// The pairs are assembled conditionally, then applied through the same
// repo-local/--global selector GPGImport uses.
func configureSSHSigning(ctx context.Context, gitr gitSigningOps, keyPath string, in SSHSigningInput) error {
	cfg := func(key, value string) error {
		if in.GitConfigGlobal {
			_, runErr := gitr.Run(ctx, "config", "--global", key, value)

			return runErr
		}

		return gitr.Config(ctx, key, value)
	}

	pairs := [][2]string{
		{"gpg.format", "ssh"},
		{"user.signingkey", keyPath},
	}

	if in.AuthorName != "" {
		pairs = append(pairs, [2]string{"user.name", in.AuthorName})
	}

	if in.AuthorEmail != "" {
		pairs = append(pairs, [2]string{"user.email", in.AuthorEmail})
	}

	if in.GitCommitSign {
		pairs = append(pairs, [2]string{"commit.gpgsign", "true"})
	}

	for _, p := range pairs {
		if err := cfg(p[0], p[1]); err != nil {
			return fmt.Errorf("ssh-signing: set %s: %w", p[0], err)
		}
	}

	return nil
}

// ensureTrailingNewline appends a newline when absent: OpenSSH refuses to read
// a private key file whose final line is not newline-terminated.
func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}

	return s + "\n"
}
