// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

// `ledger add --capture-digest` used to contact the registry first and validate
// the entry afterwards. Everything the operator got wrong in their flags — a
// misspelled SBOM path, a final tag outside the release scope, a provenance
// document that is not an object — was therefore reported only after a network
// round-trip, and only if that round-trip succeeded. When it did not, the first
// error they saw was the registry's: an auth failure or a timeout, pointing at
// the wrong thing entirely.
//
// ValidateBeforeCapture checks everything that does not depend on the digest
// the registry is about to hand back. The two fields it cannot check are the
// two the capture supplies, and Validate still checks those afterwards.

func TestValidateBeforeCapture_CatchesWhatTheFlagsAlreadyDecided(t *testing.T) {
	t.Parallel()

	const releaseTag = "v1.2.3"

	valid := func() imageledger.Entry {
		return imageledger.Entry{
			CandidateTag: "registry.invalid/owner/image:staging-v1.2.3",
			FinalTag:     "registry.invalid/owner/image:v1.2.3",
			SBOM:         "dist/image-sbom.cyclonedx.json",
		}
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*imageledger.Entry)
		wantErr bool
		why     string
	}{
		{
			name: "an entry whose only missing pieces are the captured ones", mutate: func(*imageledger.Entry) {},
			why: "ref and digest arrive from the registry; their absence must not fail the early pass",
		},
		{
			name: "an SBOM path that is not a CycloneDX document", wantErr: true,
			mutate: func(e *imageledger.Entry) { e.SBOM = "dist/image-sbom.json" },
			why:    "a typo in a flag, knowable without contacting anything",
		},
		{
			name: "a final tag outside the release scope", wantErr: true,
			mutate: func(e *imageledger.Entry) { e.FinalTag = "registry.invalid/owner/image:v9.9.9" },
			why:    "release scoping is decided by the flags alone",
		},
		{
			name: "a malformed SBOM pin", wantErr: true,
			mutate: func(e *imageledger.Entry) { e.SBOMSHA256 = "not-a-sha" },
			why:    "the pin is an operator-supplied literal",
		},
		{
			name: "a candidate tag that is not the staging counterpart", wantErr: true,
			mutate: func(e *imageledger.Entry) { e.CandidateTag = "registry.invalid/owner/image:something-else" },
			why:    "the candidate discipline compares two flags",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			entry := valid()
			tc.mutate(&entry)

			err := entry.ValidateBeforeCapture(releaseTag)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v: %s", err, tc.wantErr, tc.why)
			}
		})
	}
}

// The early pass must not claim more than it can know. Ref and digest come from
// the capture, so an entry missing them passes here and is still checked by
// Validate afterwards — otherwise moving the check earlier would have made
// --capture-digest impossible to use at all.
func TestValidateBeforeCapture_LeavesTheCapturedFieldsToValidate(t *testing.T) {
	t.Parallel()

	const releaseTag = "v1.2.3"

	entry := imageledger.Entry{
		CandidateTag: "registry.invalid/owner/image:staging-v1.2.3",
		FinalTag:     "registry.invalid/owner/image:v1.2.3",
	}

	if err := entry.ValidateBeforeCapture(releaseTag); err != nil {
		t.Fatalf("the early pass rejected an entry whose ref and digest are still to be captured: %v", err)
	}

	err := entry.Validate(releaseTag)
	if err == nil {
		t.Fatal("Validate accepted an entry with no ref or digest; the early pass would then be the only check")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("Validate err = %v, want a classified validation refusal", err)
	}
}
