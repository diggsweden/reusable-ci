// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/stretchr/testify/require"
)

// WithLock serialises a read-modify-write of a file that several jobs may touch
// at once — the image ledger is the reason it exists. Acquisition failing is
// the case that had never been reached through the real orchestration, because
// flock on a valid descriptor does not fail on demand.
//
// What matters is not what flock returns. It is what WithLock does next: a run
// that could not take the lock must not run the callback anyway. Running it
// unserialised is precisely the corruption the mechanism prevents, and it would
// look like a successful run.

var (
	errAcquireRefused = errors.New("lock could not be acquired") //nolint:err113 // injected identity is the contract.
	errReleaseFailed  = errors.New("lock could not be released") //nolint:err113 // injected identity is the contract.
	errCallbackFailed = errors.New("the guarded work failed")    //nolint:err113 // injected identity is the contract.
)

func TestWithLock_AFailedAcquisitionDoesNotRunTheGuardedWork(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	ran := false
	released := false

	err := cliio.WithLockUsingForTest(path,
		func(*os.File) error { return errAcquireRefused },
		func(*os.File) error {
			released = true

			return nil
		},
		func() error {
			ran = true

			return nil
		},
	)

	require.Error(t, err, "a run that could not take the lock reported success")
	require.ErrorIs(t, err, errAcquireRefused, "the acquisition cause was replaced")
	require.False(t, ran, "the guarded work ran without the lock; that is the corruption the lock prevents")
	require.False(t, released, "a lock that was never acquired was released")
}

// The lock is released whatever the guarded work did. A lock left held outlives
// the process only until it exits, but within one process it deadlocks the next
// caller, and the ledger flow takes this lock more than once per run.
func TestWithLock_ReleasesAfterEveryCallbackOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		callbackErr error
		why         string
	}{
		{name: "the work succeeded", why: "the ordinary path"},
		{name: "the work failed", callbackErr: errCallbackFailed, why: "a failing callback must not strand the lock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "ledger.json")
			require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

			acquired, released := 0, 0

			err := cliio.WithLockUsingForTest(path,
				func(*os.File) error {
					acquired++

					return nil
				},
				func(*os.File) error {
					released++

					return nil
				},
				func() error { return tc.callbackErr },
			)

			require.Equal(t, 1, acquired)
			require.Equalf(t, 1, released, "the lock was not released: %s", tc.why)

			if tc.callbackErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIsf(t, err, tc.callbackErr, "the callback's cause was replaced: %s", tc.why)
		})
	}
}

// A release failure is deliberately not reported, and that is worth pinning
// rather than leaving to a reader's guess: the guarded work has already
// finished, its result is what the caller asked for, and turning "the lock
// could not be released" into a failure would fail a run that did everything
// right. The descriptor closes immediately afterwards, which drops the lock
// regardless.
func TestWithLock_AReleaseFailureDoesNotChangeTheResult(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	err := cliio.WithLockUsingForTest(path,
		func(*os.File) error { return nil },
		func(*os.File) error { return errReleaseFailed },
		func() error { return nil },
	)

	require.NoError(t, err, "a run that did its work reported failure because unlocking complained")
}

// TestWithLock_AFailedCallbackLeavesTheLockTakeable is the guarantee the fake
// acquire/release pairs above cannot give.
//
// They count calls on substituted primitives, which shows WithLock invokes
// release; it does not show that a later caller can actually take the lock.
// That is the property the ledger flow depends on — it takes this lock more
// than once per run — and a lock stranded by a failing callback deadlocks the
// next caller rather than failing it, so the symptom is a job that hangs until
// the runner times it out.
//
// Real primitives, and a bounded wait: without the bound a regression here
// hangs the package instead of reporting, which is the same failure this test
// exists to catch, only in the test suite.
func TestWithLock_AFailedCallbackLeavesTheLockTakeable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	first := cliio.WithLock(path, func() error { return errCallbackFailed })
	require.ErrorIs(t, first, errCallbackFailed)

	ran := make(chan error, 1)

	go func() {
		ran <- cliio.WithLock(path, func() error {
			return os.WriteFile(path, []byte(`{"second":true}`), 0o600)
		})
	}()

	select {
	case err := <-ran:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the second caller never acquired the lock; the failed callback stranded it")
	}

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"second":true}`, string(body), "the second caller acquired but its work did not land")
}
