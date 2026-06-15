// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
)

// fakeResolver maps refs → digests; an absent ref yields an error.
type fakeResolver map[string]string

func (f fakeResolver) ResolveDigest(_ context.Context, ref string) (string, error) {
	d, ok := f[ref]
	if !ok {
		return "", errors.New("ref not found") //nolint:err113 // test mock error
	}

	return d, nil
}

func TestVerify_AcceptsWhenCandidateResolves(t *testing.T) {
	t.Parallel()

	e := validEntry()
	e.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3"

	resolver := fakeResolver{e.CandidateTag: goodDigest}

	if err := imageledger.Verify(context.Background(), resolver, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("verify rejected a matching candidate: %v", err)
	}
}

func TestVerify_FallsBackToFinalTagThenRef(t *testing.T) {
	t.Parallel()

	e := validEntry()
	e.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3"

	// Candidate already cleaned up (resolver errors on it); final_tag serves it.
	resolver := fakeResolver{e.FinalTag: goodDigest}

	if err := imageledger.Verify(context.Background(), resolver, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("verify should fall back to final_tag: %v", err)
	}
}

func TestVerify_RefusesDigestMismatch(t *testing.T) {
	t.Parallel()

	e := validEntry()

	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	resolver := fakeResolver{e.FinalTag: otherDigest, e.Ref: otherDigest}

	err := imageledger.Verify(context.Background(), resolver, []imageledger.Entry{e}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("digest mismatch should be a validation error, got %v", err)
	}
}

func TestVerify_RefusesWhenNothingResolves(t *testing.T) {
	t.Parallel()

	e := validEntry()

	err := imageledger.Verify(context.Background(), fakeResolver{}, []imageledger.Entry{e}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("unresolvable entry should be a validation error, got %v", err)
	}
}

func TestVerify_RevalidatesBeforeResolving(t *testing.T) {
	t.Parallel()

	bad := validEntry()
	bad.FinalTag = "codeberg.org/itiquette/gommitlint:v9.9.9" // out of scope

	// Even though a resolver would match, the strict re-validation fails first.
	resolver := fakeResolver{bad.FinalTag: goodDigest}

	err := imageledger.Verify(context.Background(), resolver, []imageledger.Entry{bad}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("out-of-scope tag should fail re-validation, got %v", err)
	}
}

func TestValidate_StrictCandidateAndMovingRules(t *testing.T) {
	t.Parallel()

	// candidate must be the exact staging counterpart of final_tag's suffix.
	suffixed := validEntry()
	suffixed.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.3-amd64"

	suffixed.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3-amd64"
	if err := suffixed.Validate("v1.2.3"); err != nil {
		t.Errorf("matching suffixed candidate rejected: %v", err)
	}

	// wrong suffix on candidate → reject.
	wrong := validEntry()
	wrong.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.3-amd64"

	wrong.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3-arm64"
	if err := wrong.Validate("v1.2.3"); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("mismatched candidate suffix should be rejected, got %v", err)
	}
}
