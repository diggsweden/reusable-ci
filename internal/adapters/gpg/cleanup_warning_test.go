// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestCleanupFailuresAreReportedAndCleanupContinues(t *testing.T) {
	// Swaps the global slog default, so no t.Parallel().
	bins := mockbinary.New(t)
	bins.Add("gpg", `printf 'delete failed' >&2; exit 1`)
	bins.Add("gpg-connect-agent", `printf 'agent stop failed' >&2; exit 1`)

	var logBuf bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	adapter := &adaptergpg.Adapter{
		GPGBin:   bins.Path("gpg"),
		AgentBin: bins.Path("gpg-connect-agent"),
	}
	ctx := context.Background()
	apprelease.GPGCleanup(ctx, adapter, "ABC123", io.Discard)

	logOutput := logBuf.String()
	for _, message := range []string{
		"failed to delete GPG secret key",
		"failed to delete GPG public key",
		"failed to stop gpg-agent",
	} {
		if !strings.Contains(logOutput, message) {
			t.Errorf("cleanup log missing %q:\n%s", message, logOutput)
		}
	}

	if got := len(bins.Invocations("gpg")); got != 2 {
		t.Errorf("gpg cleanup invocations = %d, want 2", got)
	}

	if got := len(bins.Invocations("gpg-connect-agent")); got != 1 {
		t.Errorf("gpg-agent cleanup invocations = %d, want 1", got)
	}
}

// TestCleanupRunsTheRightOperationsInOrder pins what cleanup actually does,
// which the counting test above cannot see: three invocations of the right
// shape would satisfy it just as well as three of the wrong shape.
//
// Three things matter here and none were asserted:
//
//   - Each deletion names the fingerprint. An argv that lost it would delete
//     nothing, or, with --batch --yes and a different selector, delete the
//     wrong key.
//   - The secret half goes first. gpg refuses to delete a public key whose
//     secret half is still present, so reversing the two leaves both keys in
//     the keyring while every command still "ran".
//   - The agent is stopped after both attempts, not between or before them.
//     Killing it first makes the deletions prompt-or-fail; the point of the
//     order is that the agent outlives the work that needs it.
func TestCleanupRunsTheRightOperationsInOrder(t *testing.T) {
	// Shares the process PATH via mockbinary, so no t.Parallel().
	bins := mockbinary.New(t)
	bins.Add("gpg", "")
	bins.Add("gpg-connect-agent", "")

	const fingerprint = "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"

	adapter := &adaptergpg.Adapter{
		GPGBin:   bins.Path("gpg"),
		AgentBin: bins.Path("gpg-connect-agent"),
	}

	apprelease.GPGCleanup(context.Background(), adapter, fingerprint, io.Discard)

	got := bins.All()
	if len(got) != 3 {
		t.Fatalf("cleanup ran %d command(s), want 3:\n%#v", len(got), got)
	}

	want := []struct {
		name string
		args []string
	}{
		{name: "gpg", args: []string{"--batch", "--yes", "--delete-secret-keys", fingerprint}},
		{name: "gpg", args: []string{"--batch", "--yes", "--delete-keys", fingerprint}},
		{name: "gpg-connect-agent", args: []string{"KILLAGENT", "/bye"}},
	}
	for i, w := range want {
		if got[i].Name != w.name {
			t.Errorf("command %d was %q, want %q", i, got[i].Name, w.name)
		}

		if !slices.Equal(got[i].Args, w.args) {
			t.Errorf("command %d argv = %q, want %q", i, got[i].Args, w.args)
		}
	}
}

// TestCleanupWithoutAFingerprintRunsNothing is the refusal half. An empty
// fingerprint means the import step never ran, and a --delete-keys with no
// selector is not a harmless no-op to hand to gpg.
func TestCleanupWithoutAFingerprintRunsNothing(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", "")
	bins.Add("gpg-connect-agent", "")

	adapter := &adaptergpg.Adapter{
		GPGBin:   bins.Path("gpg"),
		AgentBin: bins.Path("gpg-connect-agent"),
	}

	var out bytes.Buffer

	apprelease.GPGCleanup(context.Background(), adapter, "", &out)

	if got := bins.All(); len(got) != 0 {
		t.Errorf("an empty fingerprint ran %d command(s):\n%#v", len(got), got)
	}

	if !strings.Contains(out.String(), "nothing to clean up") {
		t.Errorf("the skip was not reported:\n%s", out.String())
	}
}
