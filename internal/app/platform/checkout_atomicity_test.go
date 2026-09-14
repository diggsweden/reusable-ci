// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// A checkout is a long sequence of external Git commands and any of them can
// fail. What the next run finds afterwards is the contract, and it differs by
// who owns the destination:
//
//   - A destination THIS run creates is built in a sibling staging directory
//     and renamed into place only on success. A failure must leave nothing at
//     the destination at all, so a retry starts from clean ground.
//   - A destination the CALLER provided belongs to them; renaming over it would
//     destroy files they put there. That case works in place and keeps the
//     partial state, and what must survive is the caller's own files.
//
// Both halves are asserted at EVERY step that can fail, not at one convenient
// one, because "atomic except at step 7" is the same as not atomic.

func TestCheckout_FailureAtAnyStepLeavesACreatedDestinationAbsent(t *testing.T) {
	t.Parallel()

	steps := checkoutStepCount(t)
	if steps < 3 {
		t.Fatalf("a checkout made %d failable calls; this matrix would prove almost nothing", steps)
	}

	// failAt is 1-indexed: it fires when the fake has recorded that many calls.
	for step := 1; step <= steps; step++ {
		t.Run("fail at call "+itoa(step), func(t *testing.T) {
			t.Parallel()

			parent := t.TempDir()
			workspace := filepath.Join(parent, "checkout")

			in := baseInput(t, "v1.0.0")
			in.Workspace = workspace

			git := &fakeCheckoutGit{failAt: step, failure: errFetchMiss}

			_, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in)
			if err == nil {
				t.Fatalf("call %d was supposed to fail", step)
			}

			if _, statErr := os.Lstat(workspace); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("destination exists after a failure at call %d: %v; a retry has to reason about a half-built checkout", step, statErr)
			}

			// Nor may the staging directory be left beside it.
			entries, readErr := os.ReadDir(parent)
			if readErr != nil {
				t.Fatal(readErr)
			}

			for _, entry := range entries {
				t.Errorf("failure at call %d left %q beside the destination", step, entry.Name())
			}

			// And the retry, the thing all of this is for, must work.
			retry := &fakeCheckoutGit{}
			if _, retryErr := appplatform.Checkout(context.Background(), checkoutGitFactory(retry), fakeoutputsink.New(t), nil, in); retryErr != nil {
				t.Errorf("retry after a failure at call %d: %v", step, retryErr)
			}

			if _, statErr := os.Stat(workspace); statErr != nil {
				t.Errorf("retry after call %d published nothing: %v", step, statErr)
			}
		})
	}
}

func TestCheckout_FailureAtAnyStepPreservesACallerSeededDestination(t *testing.T) {
	t.Parallel()

	const seededName = "caller-put-this-here.txt"

	const seededBody = "the caller's own file\n"

	steps := checkoutStepCount(t)

	// failAt is 1-indexed: it fires when the fake has recorded that many calls.
	for step := 1; step <= steps; step++ {
		t.Run("fail at call "+itoa(step), func(t *testing.T) {
			t.Parallel()

			workspace := t.TempDir()
			seeded := filepath.Join(workspace, seededName)

			if err := os.WriteFile(seeded, []byte(seededBody), 0o600); err != nil {
				t.Fatal(err)
			}

			in := baseInput(t, "v1.0.0")
			in.Workspace = workspace

			git := &fakeCheckoutGit{failAt: step, failure: errFetchMiss}

			if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err == nil {
				t.Fatalf("call %d was supposed to fail", step)
			}

			body, readErr := os.ReadFile(seeded) //nolint:gosec // owned temporary workspace.
			if readErr != nil {
				t.Fatalf("failure at call %d destroyed the caller's file: %v", step, readErr)
			}

			if string(body) != seededBody {
				t.Errorf("failure at call %d rewrote the caller's file to %q", step, body)
			}

			// The partial state is kept on purpose here, so the contract is
			// that a retry still succeeds rather than that the tree is clean.
			retry := &fakeCheckoutGit{}
			if _, retryErr := appplatform.Checkout(context.Background(), checkoutGitFactory(retry), fakeoutputsink.New(t), nil, in); retryErr != nil {
				t.Errorf("retry into the caller's workspace after a failure at call %d: %v", step, retryErr)
			}
		})
	}
}

// TestCheckout_RefusesToPublishFromAReplacedDirectory is the descriptor-lifetime
// half, and it is worth being precise about what it does and does not claim.
//
// Git takes a pathname. The directory guarded before the run is therefore not
// provably the directory Git wrote to: anything able to write the parent can
// swap the path in between, and no amount of checking beforehand fixes that.
// What IS available is noticing. The guarded directory's inode is recorded, and
// a checkout is not published from a path that no longer resolves to it.
//
// This is detection, not prevention, and the difference matters: work may
// already have been written into the attacker's directory. What it stops is the
// run then reporting success and publishing that work as the caller's checkout.
func TestCheckout_RefusesToPublishFromAReplacedDirectory(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "checkout")

	in := baseInput(t, "v1.0.0")
	in.Workspace = workspace

	swapped := false
	git := &fakeCheckoutGit{onCall: func(_ context.Context, _ checkoutEvent) error {
		if swapped {
			return nil
		}

		swapped = true

		// Replace the staging directory with a DIFFERENT directory at the same
		// path, by renaming one over it. That is what a hostile swap looks
		// like — a symlink or a rename — and it is also what makes this
		// deterministic: a renamed directory is a different inode, whereas
		// deleting and recreating one at the same path can reuse the inode and
		// alias to the original. The product comment records that limit.
		entries, err := os.ReadDir(parent)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			staging := filepath.Join(parent, entry.Name())

			//nolint:usetesting // must be a sibling of the staging directory, which t.TempDir() is not.
			replacement, err := os.MkdirTemp(parent, "planted-")
			if err != nil {
				return err
			}

			if err := os.WriteFile(filepath.Join(replacement, "planted"), []byte("not the checkout\n"), 0o600); err != nil {
				return err
			}

			if err := os.RemoveAll(staging); err != nil {
				return err
			}

			if err := os.Rename(replacement, staging); err != nil {
				return err
			}
		}

		return nil
	}}

	_, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in)
	if err == nil {
		t.Fatal("a checkout published from a directory that was swapped after it was checked")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want a classified validation refusal", err)
	}

	if !strings.Contains(err.Error(), "was replaced after it was checked") {
		t.Errorf("err = %v, want the diagnostic to say what happened", err)
	}

	if _, statErr := os.Lstat(workspace); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("the planted directory was published as the caller's checkout")
	}
}

// checkoutStepCount runs one successful checkout to learn how many failable
// calls it makes, so the matrices above cover every step rather than a number
// someone typed and stopped updating.
func checkoutStepCount(t *testing.T) int {
	t.Helper()

	git := &fakeCheckoutGit{}

	in := baseInput(t, "v1.0.0")
	if _, err := appplatform.Checkout(context.Background(), checkoutGitFactory(git), fakeoutputsink.New(t), nil, in); err != nil {
		t.Fatal(err)
	}

	return len(git.events)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	digits := ""
	for ; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}

	return digits
}
