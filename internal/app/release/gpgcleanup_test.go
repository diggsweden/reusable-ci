// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"io"
	"slices"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
)

type recordingGPGCleaner struct {
	calls []string
}

func (r *recordingGPGCleaner) DeleteSecretKey(_ context.Context, fingerprint string) {
	r.calls = append(r.calls, "delete-secret-key "+fingerprint)
}

func (r *recordingGPGCleaner) DeleteKey(_ context.Context, fingerprint string) {
	r.calls = append(r.calls, "delete-key "+fingerprint)
}

func (r *recordingGPGCleaner) KillAgent(context.Context) {
	r.calls = append(r.calls, "kill-agent")
}

// TestGPGCleanup_DeletesBothKeysThenStopsTheAgent pins the whole cleanup
// sequence without a gpg binary. The integration test can only observe that
// the secret key is gone afterwards; it cannot see whether the public key was
// deleted or the agent stopped, and the agent is what keeps the unlocked key
// in memory on a reused runner. The secret key goes first because gpg refuses
// to delete a public key while its secret key is still present.
func TestGPGCleanup_DeletesBothKeysThenStopsTheAgent(t *testing.T) {
	t.Parallel()

	const fingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"

	cleaner := &recordingGPGCleaner{}
	apprelease.GPGCleanup(context.Background(), cleaner, fingerprint, io.Discard)

	want := []string{"delete-secret-key " + fingerprint, "delete-key " + fingerprint, "kill-agent"}
	if !slices.Equal(cleaner.calls, want) {
		t.Errorf("calls = %v, want %v", cleaner.calls, want)
	}
}

// TestGPGCleanup_EmptyFingerprintTouchesNothing covers the skipped-import
// case: with no fingerprint there is no key of ours to delete, and stopping
// an agent this step did not start is not its business either.
func TestGPGCleanup_EmptyFingerprintTouchesNothing(t *testing.T) {
	t.Parallel()

	cleaner := &recordingGPGCleaner{}
	apprelease.GPGCleanup(context.Background(), cleaner, "", io.Discard)

	if len(cleaner.calls) != 0 {
		t.Errorf("calls = %v, want none", cleaner.calls)
	}
}
