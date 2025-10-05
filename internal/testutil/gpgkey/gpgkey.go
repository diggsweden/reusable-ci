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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// Cross-process GPG lock state.
//
// The stated reason used to be that parallel test packages share one on-disk
// GNUPGHOME. That is no longer true — every key gets its own GNUPGHOME under
// t.TempDir(), and cleanup kills only its own agent by --homedir. The lock is
// still necessary, which was established by removing it and running the three
// GPG-using packages in parallel: the second repetition hung and every package
// timed out at ten minutes. Something below the homedir boundary serialises
// badly (gpg-agent and dirmngr share a per-user socket directory), so the lock
// stays, and the reason is recorded as measured rather than assumed.
//
// It is a lock between PROCESSES — `go test` runs one binary per package — so
// the file has to be at a path they all agree on, and a mutex would not do. It
// is scoped to the user because two people on one machine have no reason to
// block each other, and because a name every user can create is a name any user
// can plant a symlink at.
//
//nolint:gochecknoglobals // process-singleton lock; intentional.
var (
	globalLockPath = filepath.Join(os.TempDir(),
		fmt.Sprintf("reusable-ci-gpgkey-%d.lock", os.Getuid()))
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

	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a GnuPG home is a directory and needs owner search permission.
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

	// Output, not CombinedOutput: gpg writes progress and warnings to stderr,
	// and this return value is used verbatim as key material. A stderr line in
	// front of the armour ("gpg: problem with fast path key listing: ...")
	// makes the key unrecognisable as armoured, and it is then misread as
	// base64 -- which fails at the colon in "gpg:", byte 3.
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			stderr = exitErr.Stderr
		}

		k.t.Fatalf("gpgkey: export armored: %v\n%s", err, stderr)
	}

	return string(out)
}

// ArmoredPublicKey returns the ASCII-armored public key (suitable for
// signature-verification fixtures).
func (k *Key) ArmoredPublicKey() string {
	k.t.Helper()
	//nolint:gosec // test infra; GNUPGHOME is t.TempDir().
	cmd := exec.CommandContext(k.t.Context(), "gpg", "--homedir", k.GNUPGHOME, "--batch", "--armor", "--export", k.Fingerprint)

	// Output, not CombinedOutput -- see ArmoredPrivateKey.
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
			stderr = exitErr.Stderr
		}

		k.t.Fatalf("gpgkey: export armored public: %v\n%s", err, stderr)
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

// cleanupTimeout bounds the agent shutdown so a wedged gpgconf cannot hang the
// test binary.
const cleanupTimeout = 10 * time.Second

func (k *Key) cleanup() {
	// Best-effort. The temp GNUPGHOME is removed by t.TempDir(). t.Context()
	// is already cancelled when cleanups run, so the shutdown gets its own
	// bounded context; with the test's context gpgconf never started and every
	// key left its agent running.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(k.t.Context()), cleanupTimeout)
	defer cancel()

	//nolint:gosec // test infra; GNUPGHOME is t.TempDir().
	_ = exec.CommandContext(ctx, "gpgconf", "--homedir", k.GNUPGHOME, "--kill", "gpg-agent").Run()
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

func lockGlobalGPG(t *testing.T) {
	t.Helper()

	globalLockMu.Lock()

	if globalLockRefs == 0 {
		file, err := openLockNoFollow(globalLockPath)
		if err != nil {
			globalLockMu.Unlock()
			t.Fatalf("gpgkey: open global lock %q: %v", globalLockPath, err)
		}

		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
			_ = file.Close()

			globalLockMu.Unlock()
			t.Fatalf("gpgkey: lock global lock %q: %v", globalLockPath, err)
		}

		globalLockFile = file
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

// openLockNoFollow opens the shared lock file, refusing a symlinked or aliased
// path.
//
// The lock lives in the shared temp directory because it has to be findable by
// every test binary in the run. That also makes it a path anyone on the machine
// can create first, and O_CREATE follows symlinks: without O_NOFOLLOW, a
// planted link would redirect both the open and the flock somewhere else
// entirely, and the tests would proceed believing they held a lock.
//
// The file is deliberately NOT removed at the end. It is the lock token itself,
// and unlinking it while another process still holds the flock would let the
// next process create a fresh file at the same path and take a second,
// independent lock — losing exactly the mutual exclusion it exists to provide.
// An empty owned file per user in the temp directory is the cheaper trade.
func openLockNoFollow(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // package-internal lock path, uid-scoped.
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		_ = file.Close()

		return nil, fmt.Errorf("lock path %s is not a regular file", path) //nolint:err113 // test infrastructure diagnostic.
	}

	return file, nil
}
