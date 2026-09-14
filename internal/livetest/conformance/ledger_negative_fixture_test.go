// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package conformance_test

// The negative controls' fixtures and the helpers they check carry no build tag
// on purpose: the wrong-digest ledger and the permission-denied judgement are
// what the live ledger and token scenarios rely on to fail, and only an
// untagged file makes an ordinary test run exercise them.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

type fixtureDigestResolver struct {
	digest string
	calls  []string
}

func (f *fixtureDigestResolver) ResolveDigest(_ context.Context, ref string) (string, error) {
	f.calls = append(f.calls, ref)

	return f.digest, nil
}

func TestLedgerNegativeFixture_RequiresObservedDigestMismatch(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	entry := imageledger.Entry{Ref: "registry.example/app@" + digest, Digest: digest, FinalTag: "registry.example/app:v1.0.0", CandidateTag: "registry.example/app:staging-v1.0.0"}
	body, err := json.Marshal([]imageledger.Entry{entry})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ledger.json")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	wrongPath := writeWrongDigestLedger(t, path, "v1.0.0")
	wrong, err := os.ReadFile(wrongPath)
	require.NoError(t, err)
	require.NotContains(t, string(wrong), digest)
	entries, err := imageledger.Parse(wrong)
	require.NoError(t, err)
	require.NoError(t, imageledger.ValidateAll(entries, "v1.0.0"))

	resolver := &fixtureDigestResolver{digest: digest}
	err = imageledger.Verify(t.Context(), resolver, entries, "v1.0.0")
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Contains(t, err.Error(), "resolves to "+digest)
	require.Equal(t, []string{entry.CandidateTag, entry.FinalTag, entries[0].Ref}, resolver.calls)
	resolver.calls = nil
	require.NoError(t, imageledger.Verify(t.Context(), resolver, []imageledger.Entry{entry}, "v1.0.0"))
	require.Equal(t, []string{entry.CandidateTag}, resolver.calls)
}

func TestTokenNegativeFixture_RequiresCanonicalPermissionDenied(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err     error
		refused bool
	}{{nil, true}, {errs.ErrUsage, true}, {errs.ErrValidation, true}, {errs.ErrMissingInput, true}, {errs.ErrDependencyUnavailable, true}, {errs.ErrPermissionDenied, false}, {fmt.Errorf("wrapped: %w", errs.ErrPermissionDenied), false}} {
		var tb refusalTB
		requirePermissionDenied(&tb, tc.err)
		require.Equal(t, tc.refused, tb.failed, "classification %v", tc.err)
	}
}

// The altered document remains structurally valid but contains no correct image
// digest. Only a registry lookup can supply the served digest in the refusal.
func writeWrongDigestLedger(t *testing.T, path, releaseTag string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := imageledger.Parse(body)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatal("wrong-digest control requires one entry")
	}

	wrong := "sha256:" + strings.Repeat("a", 64)
	if wrong == entries[0].Digest {
		wrong = "sha256:" + strings.Repeat("b", 64)
	}

	entries[0].Digest = wrong
	base, _, _ := strings.Cut(entries[0].Ref, "@")

	entries[0].Ref = base + "@" + wrong
	if validationErr := imageledger.ValidateAll(entries, releaseTag); validationErr != nil {
		t.Fatal(validationErr)
	}

	body, err = json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(filepath.Dir(path), "wrong-digest.json")
	if err := os.WriteFile(destination, body, 0o600); err != nil { //nolint:gosec // sibling of the fixture's owned ledger, never an operator path.
		t.Fatal(err)
	}

	return destination
}

func requirePermissionDenied(tb interface {
	Errorf(format string, args ...any)
}, err error) {
	if !errors.Is(err, errs.ErrPermissionDenied) {
		tb.Errorf("refused credential must be classified as permission denied: %v", err)
	}
}
