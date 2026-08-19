// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

const testFingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// mockSSHTooling stands in for ssh-keyscan and ssh-keygen. keyscanOut is
// what the scan returns; fingerprint is what `ssh-keygen -lf` reports for
// it, so a test can make the two disagree.
func mockSSHTooling(t *testing.T, keyscanOut, fingerprint string) {
	t.Helper()

	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("ssh-keyscan", "printf '%s\\n' "+shellQuote(keyscanOut))
	m.Add("ssh-keygen", `
case "$1" in
  -y) printf 'ssh-ed25519 AAAAPUBLIC generated\n' ;;
  -lf) printf '256 `+fingerprint+` host (ED25519)\n' ;;
esac
`)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// TestSetupChangelogReleaseSSH_PinsTheHostKey covers the MITM defence on
// the release-tag push: the scanned host key is fingerprinted and must
// equal the fingerprint the caller pinned, or the setup aborts.
//
// None of this had a test. A mismatch that was accepted would let a
// man-in-the-middle take the push — and the private signing key is
// already on disk by the time the scan runs, so the cleanup on that path
// matters as much as the refusal.
func TestSetupChangelogReleaseSSH_PinsTheHostKey(t *testing.T) {
	t.Run("fingerprint mismatch is refused and cleans up", func(t *testing.T) {
		mockSSHTooling(t, "codeberg.org ssh-ed25519 AAAAHOSTKEY", "SHA256:DIFFERENTDIFFERENTDIFFERENTDIFFERENTDIFF")
		t.Setenv("RUNNER_TEMP", t.TempDir())

		_, _, cleanup, err := setupChangelogReleaseSSH(context.Background(), "PRIVATE-KEY-BODY", "codeberg.org", 22, "ed25519", testFingerprint)
		defer cleanup()

		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}

		// The message names both fingerprints so an operator can tell a
		// rotation from an attack.
		if !strings.Contains(err.Error(), testFingerprint) {
			t.Errorf("error should name the expected fingerprint: %v", err)
		}

		// The signing key was written before the scan; a refused setup
		// must not leave it behind.
		assertNoLeftoverSSHDir(t, os.Getenv("RUNNER_TEMP"))
	})

	t.Run("matching fingerprint yields a pinned, locked-down config", func(t *testing.T) {
		mockSSHTooling(t, "codeberg.org ssh-ed25519 AAAAHOSTKEY", testFingerprint)
		t.Setenv("RUNNER_TEMP", t.TempDir())

		sshCommand, keyPath, cleanup, err := setupChangelogReleaseSSH(context.Background(), "PRIVATE-KEY-BODY", "codeberg.org", 22, "ed25519", testFingerprint)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		defer cleanup()

		// The private key is owner-only.
		info, statErr := os.Stat(keyPath)
		if statErr != nil {
			t.Fatal(statErr)
		}

		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("signing key mode = %v, want 0600", perm)
		}

		config, readErr := os.ReadFile(filepath.Join(filepath.Dir(keyPath), "config"))
		if readErr != nil {
			t.Fatal(readErr)
		}

		// These four lines are the point of generating a config at all:
		// without them ssh would accept an unknown host key, fall back to
		// an agent key, or read the user's own ssh config.
		for _, want := range []string{
			"StrictHostKeyChecking yes",
			"IdentitiesOnly yes",
			"UserKnownHostsFile " + filepath.Join(filepath.Dir(keyPath), "known_hosts"),
			"IdentityFile " + keyPath,
		} {
			if !strings.Contains(string(config), want) {
				t.Errorf("ssh config missing %q:\n%s", want, config)
			}
		}

		// GIT_SSH_COMMAND must point at the generated config, or none of
		// the above applies to the push.
		if !strings.Contains(sshCommand, "-F") || !strings.Contains(sshCommand, filepath.Dir(keyPath)) {
			t.Errorf("GIT_SSH_COMMAND does not use the generated config: %q", sshCommand)
		}

		// And cleanup really removes the key.
		cleanup()
		assertNoLeftoverSSHDir(t, os.Getenv("RUNNER_TEMP"))
	})
}

func assertNoLeftoverSSHDir(t *testing.T, base string) {
	t.Helper()

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "reusable-ci-ssh.") {
			t.Errorf("ssh temp dir %q was left behind", e.Name())
		}
	}
}
