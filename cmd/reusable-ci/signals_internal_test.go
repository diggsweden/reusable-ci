// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

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

// TestWatchSignals_FirstSignalWarnsAndCancels covers the arm that runs when a
// signal actually arrives, which nothing executed.
//
// The existing test cancels the context programmatically, taking the other
// branch of the same select, so the interrupt notice and the cancel it
// triggers were never observed. That cancel is how every in-flight operation
// learns to unwind: without it Ctrl-C prints a message and the run continues
// to completion.
//
// The watcher is then required to terminate. Left parked on the second receive
// it lives for the rest of the test binary, which is the leak this replaces.
func TestWatchSignals_FirstSignalWarnsAndCancels(t *testing.T) {
	// Substitutes the package-level exit, so no t.Parallel().
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	sigCh := make(chan os.Signal, 2)

	var stderr bytes.Buffer

	exited := make(chan int, 1)

	previous := exitProcess

	exitProcess = func(code int) { exited <- code }

	t.Cleanup(func() { exitProcess = previous })

	done := make(chan struct{})

	go func() {
		watchSignals(ctx, sigCh, cancel, &stderr)
		close(done)
	}()

	sigCh <- os.Interrupt

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the first signal did not cancel the context, so nothing in flight would unwind")
	}

	if got := stderr.String(); !strings.Contains(got, "interrupted") || !strings.Contains(got, "force-quit") {
		t.Errorf("stderr = %q, want the interrupt notice naming the force-quit follow-up", got)
	}

	// Second signal: the force-quit path. It must report and exit 130 --
	// SIGINT's conventional 128+2 -- and not fall through to anything else.
	sigCh <- os.Interrupt

	select {
	case code := <-exited:
		if code != exitInterrupted {
			t.Errorf("force-quit exit code = %d, want %d", code, exitInterrupted)
		}
	case <-time.After(time.Second):
		t.Fatal("the second signal did not force-quit")
	}

	if !strings.Contains(stderr.String(), "Force-quit.") {
		t.Errorf("stderr = %q, want the force-quit notice", stderr.String())
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchSignals never returned; the watcher goroutine outlives the test")
	}
}
