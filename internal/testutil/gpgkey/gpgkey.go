// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gpgkey generates a throwaway GPG key in an isolated GNUPGHOME for
// the duration of a test. Replaces the per-test setup blocks in
// tests/release/import-gpg-key.bats and tests/release/cleanup-gpg-key.bats.
//
// The key has no passphrase and uses an ed25519 sign-only primary, which is
// fast to generate (~50ms) and keeps tests under one second each.
//
// On t.Cleanup the gpg-agent for the isolated GNUPGHOME is killed and the
// keyring is removed. No real-developer keyring is ever touched.
package gpgkey

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

// Process-wide GPG keyring lock state — must be package-level because
// multiple parallel test packages share the same on-disk GNUPGHOME.
//
//nolint:gochecknoglobals // process-singleton lock; intentional.
var (
	globalLockPath = filepath.Join(os.TempDir(), "reusable-ci-gpgkey.lock")
	globalLockMu   sync.Mutex
	globalLockFile *os.File
	globalLockRefs int
)

// Key holds the throwaway key's metadata and the GNUPGHOME it lives in.
type Key struct {
	t           *testing.T
	GNUPGHOME   string
	Name        string
	Email       string
	UID         string
	Fingerprint string
}

// New generates a throwaway key in t.TempDir(), exports the metadata,
// and registers cleanup that kills gpg-agent + removes the keyring.
func New(t *testing.T) *Key {
	t.Helper()
	lockGlobalGPG(t)

	env := testenv.New(t)

	dir := filepath.Join(env.Home, ".gnupg")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("gpgkey: mkdir %q: %v", dir, err)
	}

	env.Setenv("GNUPGHOME", dir)

	name := "Reusable CI Test"
	email := "ci-test@example.invalid"
	uid := name + " <" + email + ">"

	if err := chmod700(dir); err != nil {
		t.Fatalf("gpgkey: chmod 700 %q: %v", dir, err)
	}

	// --quick-generate-key creates a key non-interactively. ed25519 + sign-only
	// is the fastest valid combination for our purposes.
	//nolint:gosec // test infra; dir and uid are test-controlled.
	cmd := exec.CommandContext(t.Context(), "gpg", "--homedir", dir, "--batch", "--quiet", "--pinentry-mode", "loopback",
		"--passphrase", "", "--quick-generate-key", uid, "ed25519", "sign", "0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gpgkey: generate: %v\n%s", err, out)
	}

	fpr := readFingerprint(t, dir)

	k := &Key{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		t:           t,
		GNUPGHOME:   dir,
		Name:        name,
		Email:       email,
		UID:         uid,
		Fingerprint: fpr,
	}
	t.Cleanup(k.cleanup)

	return k
}

// ArmoredPrivateKey returns the ASCII-armored secret key (suitable as
// the GPG_PRIVATE_KEY input for adapter/gpg.Import).
func (k *Key) ArmoredPrivateKey() string {
	k.t.Helper()
	//nolint:gosec // test infra; GNUPGHOME is t.TempDir().
	cmd := exec.CommandContext(k.t.Context(), "gpg", "--homedir", k.GNUPGHOME, "--batch", "--armor", "--export-secret-keys", k.Fingerprint)

	out, err := cmd.CombinedOutput()
	if err != nil {
		k.t.Fatalf("gpgkey: export armored: %v\n%s", err, out)
	}

	return string(out)
}

// ArmoredPublicKey returns the ASCII-armored public key (suitable for
// signature-verification fixtures).
func (k *Key) ArmoredPublicKey() string {
	k.t.Helper()
	//nolint:gosec // test infra; GNUPGHOME is t.TempDir().
	cmd := exec.CommandContext(k.t.Context(), "gpg", "--homedir", k.GNUPGHOME, "--batch", "--armor", "--export", k.Fingerprint)

	out, err := cmd.CombinedOutput()
	if err != nil {
		k.t.Fatalf("gpgkey: export armored public: %v\n%s", err, out)
	}

	return string(out)
}

// KeyID returns the long key ID (last 16 hex chars of the fingerprint).
func (k *Key) KeyID() string {
	if len(k.Fingerprint) < 16 {
		return k.Fingerprint
	}

	return k.Fingerprint[len(k.Fingerprint)-16:]
}

func (k *Key) cleanup() {
	// Best-effort. The temp GNUPGHOME is removed by t.TempDir().
	//nolint:gosec // test infra; GNUPGHOME is t.TempDir().
	_ = exec.CommandContext(k.t.Context(), "gpgconf", "--homedir", k.GNUPGHOME, "--kill", "gpg-agent").Run()
}

func readFingerprint(t *testing.T, homedir string) string {
	t.Helper()

	//nolint:gosec // test infra; homedir is test-controlled.
	cmd := exec.CommandContext(t.Context(), "gpg", "--homedir", homedir, "--batch", "--with-colons", "--list-secret-keys")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gpgkey: list-secret-keys: %v", err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "fpr:") {
			f := strings.Split(line, ":")
			if len(f) >= 10 && f[9] != "" {
				return f[9]
			}
		}
	}

	t.Fatalf("gpgkey: no fingerprint in:\n%s", out)

	return ""
}

func chmod700(dir string) error {
	//nolint:gosec,noctx // best-effort cleanup helper without t.Context().
	return exec.Command("chmod", "700", dir).Run()
}

func lockGlobalGPG(t *testing.T) {
	t.Helper()

	globalLockMu.Lock()
	if globalLockRefs == 0 {
		f, err := os.OpenFile(globalLockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec,varnamelen // test infra global lock; path is a package-internal constant.
		if err != nil {
			globalLockMu.Unlock()
			t.Fatalf("gpgkey: open global lock %q: %v", globalLockPath, err)
		}

		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			_ = f.Close()
			globalLockMu.Unlock()
			t.Fatalf("gpgkey: lock global lock %q: %v", globalLockPath, err)
		}

		globalLockFile = f
	}

	globalLockRefs++
	globalLockMu.Unlock()

	t.Cleanup(func() {
		globalLockMu.Lock()
		defer globalLockMu.Unlock()

		globalLockRefs--
		if globalLockRefs > 0 {
			return
		}

		if globalLockFile == nil {
			return
		}

		_ = syscall.Flock(int(globalLockFile.Fd()), syscall.LOCK_UN)
		_ = globalLockFile.Close()
		globalLockFile = nil
	})
}
