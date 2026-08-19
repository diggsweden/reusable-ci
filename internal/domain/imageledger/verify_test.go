// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
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

// TestVerify_ChecksEveryEntry covers the loop. A ledger carries one entry
// per built image, and every test above passes a slice of one -- so a
// Verify that stopped after the first entry would satisfy all of them
// while waving through every image behind it. That is the whole point of
// re-verifying at the trust boundary: the build and sign stages are
// separate, and this is what stops an entry the registry does not back
// from being signed.
func TestVerify_ChecksEveryEntry(t *testing.T) {
	t.Parallel()

	good := validEntry()

	// A second image in the same release, whose recorded digest the
	// registry does not serve under any of its refs.
	bad := validEntry()
	bad.Role = "static"
	bad.Ref = "codeberg.org/itiquette/gommitlint-static@" + goodDigest
	bad.FinalTag = "codeberg.org/itiquette/gommitlint-static:v1.2.3"

	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	resolver := fakeResolver{
		good.FinalTag: goodDigest,
		good.Ref:      goodDigest,
		bad.FinalTag:  otherDigest,
		bad.Ref:       otherDigest,
	}

	for _, tc := range []struct {
		name    string
		entries []imageledger.Entry
	}{
		{name: "bad entry last", entries: []imageledger.Entry{good, bad}},
		{name: "bad entry first", entries: []imageledger.Entry{bad, good}},
		{name: "bad entry in the middle", entries: []imageledger.Entry{good, bad, good}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := imageledger.Verify(context.Background(), resolver, tc.entries, "v1.2.3")
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			// The message names which entry failed; with several images
			// in a release, "something did not verify" is not actionable.
			if !strings.Contains(err.Error(), "gommitlint-static") {
				t.Errorf("error should name the failing image: %v", err)
			}
		})
	}

	// The all-good ledger still passes, so the cases above fail for the
	// reason claimed rather than because multiple entries break Verify.
	if err := imageledger.Verify(context.Background(), resolver, []imageledger.Entry{good, good}, "v1.2.3"); err != nil {
		t.Errorf("all-matching ledger rejected: %v", err)
	}
}
