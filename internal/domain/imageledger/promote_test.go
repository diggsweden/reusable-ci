// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// fakeRegistry is a mutable ref→digest map with a CopyTag that points
// dest at source's digest, modelling a correct registry copy. badCopy
// overrides the digest a copy lands, modelling a wrong-image promotion.
type fakeRegistry struct {
	digests map[string]string
	badCopy string // when set, CopyTag lands this digest instead of source's
	copies  []string
}

func (r *fakeRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	d, ok := r.digests[ref]
	if !ok {
		return "", errors.New("ref not found") //nolint:err113 // test mock error
	}

	return d, nil
}

func (r *fakeRegistry) CopyTag(_ context.Context, source, dest string) error {
	r.copies = append(r.copies, source+"->"+dest)

	landed := r.digests[source]
	if r.badCopy != "" {
		landed = r.badCopy
	}

	r.digests[dest] = landed

	return nil
}

// fakeSigCopier models a signature-preserving cross-registry copy: dest
// lands source's digest (like a correct `cosign copy`), recording the call.
type fakeSigCopier struct {
	reg    *fakeRegistry
	copies []string
}

func (c *fakeSigCopier) CopyWithSignatures(_ context.Context, source, dest string) error {
	c.copies = append(c.copies, source+"=>"+dest)
	c.reg.digests[dest] = c.reg.digests[source]

	return nil
}

func candidateEntry() imageledger.Entry {
	e := validEntry()
	e.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3"

	return e
}

func TestPromote_CopiesCandidateToReleasePointerAndVerifies(t *testing.T) {
	t.Parallel()

	const releasePointer = "codeberg.org/itiquette/gommitlint:release"

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	if err := imageledger.Promote(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("promote failed: %v", err)
	}

	// The :release pointer now serves the candidate digest.
	if reg.digests[releasePointer] != goodDigest {
		t.Errorf("release pointer not promoted: %v", reg.digests)
	}

	// The immutable :<version> tag is build-applied, never written by promote.
	if _, ok := reg.digests[e.FinalTag]; ok {
		t.Errorf("release promotion must not write the immutable %s, got %v", e.FinalTag, reg.digests)
	}

	if len(reg.copies) != 1 {
		t.Errorf("expected 1 copy (the :release pointer), got %v", reg.copies)
	}
}

func TestPromoteToStage_ReleaseCanUseLedgerFinalAndMovingTags(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("release-tag promote failed: %v", err)
	}

	if reg.digests[e.FinalTag] != goodDigest {
		t.Errorf("final tag not promoted: %v", reg.digests)
	}

	if reg.digests[e.MovingTag] != goodDigest {
		t.Errorf("moving tag not promoted: %v", reg.digests)
	}

	if len(reg.copies) != 2 {
		t.Errorf("expected final+moving copies, got %v", reg.copies)
	}
}

func TestPromoteToStage_ReleaseRefusesExistingFinalDifferentDigest(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	reg := &fakeRegistry{digests: map[string]string{
		e.CandidateTag: goodDigest,
		e.FinalTag:     otherDigest,
	}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}

	err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("existing final tag with another digest must be refused, got %v", err)
	}

	if len(reg.copies) != 0 {
		t.Errorf("no tag should be copied after immutable-final conflict, got %v", reg.copies)
	}
}

func TestPromoteToStage_ReleaseSkipsExistingFinalAtSameDigest(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	reg := &fakeRegistry{digests: map[string]string{
		e.CandidateTag: goodDigest,
		e.FinalTag:     goodDigest,
	}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("release-tag promote failed: %v", err)
	}

	want := []string{e.CandidateTag + "->" + e.MovingTag}
	if !slices.Equal(reg.copies, want) {
		t.Errorf("expected only moving-tag copy when final already serves digest, got %v", reg.copies)
	}
}

func TestPromoteToStage_DefaultStillRequiresCandidate(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.Ref: goodDigest}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err == nil {
		t.Fatal("default promotion should still fail when candidate_tag is absent from the registry")
	}

	if len(reg.copies) != 0 {
		t.Errorf("no copy should happen without explicit digest-ref fallback, got %v", reg.copies)
	}
}

func TestPromoteToStage_DigestRefFallbackUsesRecordedRefWhenCandidateMissing(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.Ref: goodDigest}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true, AllowDigestRefFallback: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("digest-ref fallback promote failed: %v", err)
	}

	if !slices.Equal(reg.copies, []string{e.Ref + "->" + e.FinalTag}) {
		t.Errorf("expected promote from digest ref, got %v", reg.copies)
	}

	if reg.digests[e.FinalTag] != goodDigest {
		t.Errorf("final tag not promoted from digest ref: %v", reg.digests)
	}
}

func TestPromoteToStage_DigestRefFallbackUsesRecordedRefWhenCandidateMoved(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: otherDigest, e.Ref: goodDigest}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true, AllowDigestRefFallback: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("digest-ref fallback promote failed: %v", err)
	}

	if !slices.Equal(reg.copies, []string{e.Ref + "->" + e.FinalTag}) {
		t.Errorf("expected promote from digest ref, got %v", reg.copies)
	}
}

func TestPromoteToStage_DigestRefFallbackPromotesEntryWithoutCandidate(t *testing.T) {
	t.Parallel()

	e := validEntry()
	reg := &fakeRegistry{digests: map[string]string{e.Ref: goodDigest}}
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true, AllowDigestRefFallback: true}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("digest-ref fallback promote failed: %v", err)
	}

	if !slices.Equal(reg.copies, []string{e.Ref + "->" + e.FinalTag}) {
		t.Errorf("expected no-candidate promote from digest ref, got %v", reg.copies)
	}
}

func TestPromoteToStage_DevPromotesCandidateToStageTag(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	// A pre-release (dev) promotion needs no release tag; it retags the
	// candidate's digest to <base>:dev and verifies — same trust boundary.
	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "", imageledger.Stage{Name: "dev"}); err != nil {
		t.Fatalf("dev promote failed: %v", err)
	}

	if reg.digests["codeberg.org/itiquette/gommitlint:dev"] != goodDigest {
		t.Errorf("dev tag not promoted to digest: %v", reg.digests)
	}

	// A dev promotion must NOT touch the final tag.
	if _, ok := reg.digests[e.FinalTag]; ok {
		t.Errorf("dev promotion must not touch final tag, got %v", reg.digests)
	}
}

func TestPromoteToStage_DevSkipsReleaseScope(t *testing.T) {
	t.Parallel()

	// final_tag is not scoped to any release tag (no release exists yet at
	// dev time); the dev promotion must still pass — release scope is only
	// enforced at the terminal release promotion.
	e := candidateEntry()
	e.FinalTag = "codeberg.org/itiquette/gommitlint:main"
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	if err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "", imageledger.Stage{Name: "dev"}); err != nil {
		t.Fatalf("dev promotion must skip release scope, got %v", err)
	}
}

func TestPromoteToStage_CrossRepoUsesSignatureCopier(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}
	sig := &fakeSigCopier{reg: reg}

	// Promote the Codeberg candidate to a ghcr release tag (cross-registry):
	// must route through the signature copier, not CopyTag, and verify. The
	// target is a registry prefix; the source path (itiquette/gommitlint) is
	// preserved beneath it, so the dest is ghcr.io/itiquette/gommitlint:release-prod.
	stage := imageledger.Stage{Name: "release-prod", TargetRepo: "ghcr.io"}
	if err := imageledger.PromoteToStage(context.Background(), reg, sig, []imageledger.Entry{e}, "", stage); err != nil {
		t.Fatalf("cross-repo promote failed: %v", err)
	}

	dest := "ghcr.io/itiquette/gommitlint:release-prod"
	if reg.digests[dest] != goodDigest {
		t.Errorf("cross-repo dest not promoted to digest: %v", reg.digests)
	}

	if len(sig.copies) != 1 {
		t.Errorf("expected 1 signature-preserving copy, got %v", sig.copies)
	}

	if len(reg.copies) != 0 {
		t.Errorf("cross-repo promotion must not use plain CopyTag, got %v", reg.copies)
	}
}

func TestPromoteToStage_CrossRepoRefusedWithoutCopier(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	// Cross-repo destination but no signature copier → refuse, don't silently
	// drop the signature.
	stage := imageledger.Stage{Name: "release-prod", TargetRepo: "ghcr.io/itiquette/gommitlint"}

	err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "", stage)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("cross-repo without a copier must be refused as usage error, got %v", err)
	}

	if len(reg.copies) != 0 {
		t.Errorf("nothing should be copied when cross-repo is refused, got %v", reg.copies)
	}
}

func TestPromoteToStage_RejectsInvalidStageName(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	err := imageledger.PromoteToStage(context.Background(), reg, nil, []imageledger.Entry{e}, "", imageledger.Stage{Name: "bad/stage"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("invalid stage name must be a usage error, got %v", err)
	}

	if len(reg.copies) != 0 {
		t.Errorf("no copy should happen for an invalid stage, got %v", reg.copies)
	}
}

func TestPromote_SkipsEntryWithoutCandidate(t *testing.T) {
	t.Parallel()

	e := validEntry() // no candidate_tag
	reg := &fakeRegistry{digests: map[string]string{}}

	if err := imageledger.Promote(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("entry without candidate should be skipped, got %v", err)
	}

	if len(reg.copies) != 0 {
		t.Errorf("nothing should be copied, got %v", reg.copies)
	}
}

func TestPromote_RefusesWrongImageLanded(t *testing.T) {
	t.Parallel()

	e := candidateEntry()

	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	reg := &fakeRegistry{
		digests: map[string]string{e.CandidateTag: goodDigest},
		badCopy: otherDigest, // copy lands the wrong digest
	}

	err := imageledger.Promote(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("a wrong-image promotion must be refused, got %v", err)
	}
}

func TestPromote_RefusesCandidateDigestMismatchBeforeCopy(t *testing.T) {
	t.Parallel()

	e := candidateEntry()

	otherDigest := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	// Candidate serves a different digest than recorded → refuse before any copy.
	reg := &fakeRegistry{digests: map[string]string{e.CandidateTag: otherDigest}}

	err := imageledger.Promote(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("candidate digest mismatch must be refused, got %v", err)
	}

	if len(reg.copies) != 0 {
		t.Errorf("no copy should happen when candidate digest is wrong, got %v", reg.copies)
	}
}
