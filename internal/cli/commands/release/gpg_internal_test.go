// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"errors"
	"os"
	"strings"
	"testing"
)

var errFromCallback = errors.New("callback failed")

// The GPG subcommands run under an ephemeral GNUPGHOME. Three things have
// to hold and none had a test: the keyring home is private (gpg refuses
// one others can read, and a private key is imported into it), the
// callback sees it, and the process environment is put back afterwards —
// this mutates a global, so a leak follows the runner into whatever runs
// next.

func TestWithEphemeralGNUPGHome_PrivateHomeUnderRunnerTemp(t *testing.T) {
	runnerTemp := t.TempDir()
	t.Setenv("RUNNER_TEMP", runnerTemp)
	t.Setenv("GNUPGHOME", "/pre-existing/home")

	var seen string

	err := withEphemeralGNUPGHome(func() error {
		seen = os.Getenv("GNUPGHOME")

		info, statErr := os.Stat(seen)
		if statErr != nil {
			return statErr
		}

		// 0700: gpg refuses a keyring home others can read.
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("GNUPGHOME mode = %v, want 0700", perm)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Created under the runner temp dir, so it is inside whatever the
	// runner cleans up rather than the shared system temp.
	if !strings.HasPrefix(seen, runnerTemp) {
		t.Errorf("GNUPGHOME %q is not under $RUNNER_TEMP %q", seen, runnerTemp)
	}

	if _, statErr := os.Stat(seen); !os.IsNotExist(statErr) {
		t.Errorf("GNUPGHOME survived the call (stat err = %v)", statErr)
	}

	if got := os.Getenv("GNUPGHOME"); got != "/pre-existing/home" {
		t.Errorf("GNUPGHOME = %q afterwards, want the pre-existing value restored", got)
	}
}

func TestWithEphemeralGNUPGHome_UnsetStaysUnset(t *testing.T) {
	t.Setenv("RUNNER_TEMP", t.TempDir())
	t.Setenv("GNUPGHOME", "placeholder") // so t.Setenv restores it afterwards

	_ = os.Unsetenv("GNUPGHOME")

	if err := withEphemeralGNUPGHome(func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	// Restoring must not invent a value where there was none: an empty
	// GNUPGHOME sends gpg to ~/.gnupg, which is the opposite of the
	// isolation this function exists to provide.
	if _, ok := os.LookupEnv("GNUPGHOME"); ok {
		t.Errorf("GNUPGHOME is now set to %q; it was unset before", os.Getenv("GNUPGHOME"))
	}
}

func TestWithEphemeralGNUPGHome_CleansUpAfterAFailedCallback(t *testing.T) {
	runnerTemp := t.TempDir()
	t.Setenv("RUNNER_TEMP", runnerTemp)
	t.Setenv("GNUPGHOME", "/pre-existing/home")

	var seen string

	err := withEphemeralGNUPGHome(func() error {
		seen = os.Getenv("GNUPGHOME")

		return errFromCallback
	})
	if !errors.Is(err, errFromCallback) {
		t.Fatalf("err = %v, want the callback's error", err)
	}

	// A failed signing run must not leave a keyring behind.
	if _, statErr := os.Stat(seen); !os.IsNotExist(statErr) {
		t.Errorf("GNUPGHOME survived a failed callback (stat err = %v)", statErr)
	}

	if got := os.Getenv("GNUPGHOME"); got != "/pre-existing/home" {
		t.Errorf("GNUPGHOME = %q after a failure, want the pre-existing value restored", got)
	}

	entries, readErr := os.ReadDir(runnerTemp)
	if readErr != nil {
		t.Fatal(readErr)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "reusable-ci-gnupg-") {
			t.Errorf("left %q behind in the runner temp dir", e.Name())
		}
	}
}
