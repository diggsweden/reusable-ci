// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatchSignals_FirstSignalCancelsAndAnnounces verifies the
// first-signal contract: ctx ends and the announcement reaches stderr.
//
// watchSignals doesn't return after one signal (it parks on the
// second-signal channel), so this test leaks the goroutine on purpose —
// matching the production lifetime where the watcher is reaped with the
// process.
func TestWatchSignals_FirstSignalCancelsAndAnnounces(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sigCh := make(chan os.Signal, 2)

	var stderr bytes.Buffer
	go watchSignals(ctx, sigCh, cancel, &stderr)

	sigCh <- os.Interrupt

	waitFor(t, "ctx cancellation", func() bool { return ctx.Err() != nil })
	waitFor(t, "stderr announcement", func() bool {
		return strings.Contains(stderr.String(), "interrupted; press Ctrl-C again to force-quit")
	})
}

// TestWatchSignals_NormalReturnIsSilent verifies that a programmatic
// ctx cancel (no signal) makes watchSignals return without writing to
// stderr.
func TestWatchSignals_NormalReturnIsSilent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 2)

	var stderr bytes.Buffer

	done := make(chan struct{})

	go func() {
		watchSignals(ctx, sigCh, cancel, &stderr)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchSignals never returned after programmatic cancel")
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

// TestWatchSignals_SecondSignalForceQuits verifies the second-signal
// contract: osExit is called with exitInterrupted.
func TestWatchSignals_SecondSignalForceQuits(t *testing.T) {
	// Can't run in parallel — overrides the package-level osExit seam.
	origExit := osExit

	t.Cleanup(func() { osExit = origExit })

	var exitCode atomic.Int32
	exitCode.Store(-1)

	osExit = func(code int) {
		exitCode.Store(int32(code)) //nolint:gosec // test seam: exit code fits in int32.
		// Real os.Exit doesn't return; emulate by parking forever.
		select {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sigCh := make(chan os.Signal, 2)

	var stderr bytes.Buffer
	go watchSignals(ctx, sigCh, cancel, &stderr)

	sigCh <- os.Interrupt

	waitFor(t, "first-signal ctx cancellation", func() bool { return ctx.Err() != nil })

	sigCh <- os.Interrupt

	waitFor(t, "osExit call", func() bool { return exitCode.Load() != -1 })

	if got := exitCode.Load(); got != int32(exitInterrupted) {
		t.Errorf("exit code = %d, want %d", got, exitInterrupted)
	}

	if !strings.Contains(stderr.String(), "Force-quit") {
		t.Errorf("stderr = %q, want Force-quit line", stderr.String())
	}
}

// waitFor polls `predicate` until it returns true or the test deadline
// fires. Used because goroutines write asynchronously and we want a
// tighter loop than a hardcoded sleep.
func waitFor(t *testing.T, what string, predicate func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(time.Millisecond)
	}
}
