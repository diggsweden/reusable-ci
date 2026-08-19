// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// PromoteBaseImages had no test file. It is the step that turns a
// candidate base image into the immutable tag every release image is
// then built on, so what it refuses matters as much as what it does.
//
// Everything here is refused before the registry is touched, which is
// the point: a promotion that should not happen must not half-happen.

const (
	promoteRepo   = "codeberg.org/itiquette/base"
	promoteDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	promoteInput  = "2222222222222222222222222222222222222222222222222222222222222222"
)

func promoteImage() BaseImageMetadata {
	return BaseImageMetadata{
		Flavor:      "rust",
		Tag:         promoteRepo + ":rust-1.0.0",
		Ref:         promoteRepo + "@" + promoteDigest,
		BaseInputID: promoteInput,
	}
}

func promoteInputFor(images ...BaseImageMetadata) BaseImagePromoteInput {
	return BaseImagePromoteInput{
		Images:             images,
		BaseInputID:        promoteInput,
		ExpectedRepository: promoteRepo,
		ExpectedSource:     "https://codeberg.org/itiquette/base",
		ExpectedWorkflow:   ".forgejo/workflows/base-images.yml",
		CosignPublicKey:    "cosign.pub",
	}
}

func TestPromoteBaseImages_RequiresItsCollaborators(t *testing.T) {
	t.Parallel()

	// Unchecked, a nil adapter is a panic rather than an error.
	if _, err := PromoteBaseImages(context.Background(), nil, &fakeBaseImageVerifier{}, io.Discard, promoteInputFor(promoteImage())); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("nil registry: err = %v, want ErrUsage", err)
	}

	if _, err := PromoteBaseImages(context.Background(), &fakeBaseImageRegistry{}, nil, io.Discard, promoteInputFor(promoteImage())); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("nil verifier: err = %v, want ErrUsage", err)
	}
}

func TestPromoteBaseImages_RequiresTheTrustInputs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		mutate  func(*BaseImagePromoteInput)
		wantErr error
	}{
		// Each of these is half of what makes the evidence check mean
		// anything: without them the promotion would verify a signature
		// without constraining who produced it or from where.
		{name: "no expected repository", mutate: func(in *BaseImagePromoteInput) { in.ExpectedRepository = "" }, wantErr: errs.ErrMissingInput},
		{name: "no expected source", mutate: func(in *BaseImagePromoteInput) { in.ExpectedSource = "" }, wantErr: errs.ErrMissingInput},
		{name: "no expected workflow", mutate: func(in *BaseImagePromoteInput) { in.ExpectedWorkflow = "" }, wantErr: errs.ErrMissingInput},
		{name: "no cosign public key", mutate: func(in *BaseImagePromoteInput) { in.CosignPublicKey = "" }, wantErr: errs.ErrMissingInput},

		{name: "base input id is not a digest", mutate: func(in *BaseImagePromoteInput) { in.BaseInputID = "not-a-digest" }, wantErr: errs.ErrValidation},

		// Promoting nothing is not a successful promotion.
		{name: "no images", mutate: func(in *BaseImagePromoteInput) { in.Images = nil }, wantErr: errs.ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := promoteInputFor(promoteImage())
			tc.mutate(&in)

			registry := &fakeBaseImageRegistry{}

			_, err := PromoteBaseImages(context.Background(), registry, &fakeBaseImageVerifier{}, io.Discard, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestPromoteBaseImages_RefusesUnusableMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*BaseImageMetadata)
	}{
		{name: "no flavor", mutate: func(m *BaseImageMetadata) { m.Flavor = "" }},
		{name: "no tag", mutate: func(m *BaseImageMetadata) { m.Tag = "" }},
		{name: "no ref", mutate: func(m *BaseImageMetadata) { m.Ref = "" }},
		{name: "flavor with a slash", mutate: func(m *BaseImageMetadata) { m.Flavor = "rust/evil" }},
		{name: "flavor with a space", mutate: func(m *BaseImageMetadata) { m.Flavor = "rust evil" }},

		// The ref must be digest-pinned: promoting a tag would bind the
		// immutable release tag to whatever that tag resolves to later.
		{name: "ref is a tag", mutate: func(m *BaseImageMetadata) { m.Ref = promoteRepo + ":rust-1.0.0" }},
		{name: "ref in another repository", mutate: func(m *BaseImageMetadata) { m.Ref = "codeberg.org/evil/base@" + promoteDigest }},
		{name: "tag in another repository", mutate: func(m *BaseImageMetadata) { m.Tag = "codeberg.org/evil/base:rust-1.0.0" }},
		{name: "tag without a tag part", mutate: func(m *BaseImageMetadata) { m.Tag = promoteRepo }},

		// The candidate pair is optional but validated when present.
		{name: "candidate ref is a tag", mutate: func(m *BaseImageMetadata) { m.CandidateRef = promoteRepo + ":staging" }},
		{name: "candidate ref in another repository", mutate: func(m *BaseImageMetadata) { m.CandidateRef = "codeberg.org/evil/base@" + promoteDigest }},
		{name: "candidate tag in another repository", mutate: func(m *BaseImageMetadata) { m.CandidateTag = "codeberg.org/evil/base:staging" }},

		// Every promoted image has to come from the same input set; a
		// mismatch means promoting an image built from something else.
		{name: "base input id disagrees with the run", mutate: func(m *BaseImageMetadata) { m.BaseInputID = strings.Repeat("3", 64) }},
		{name: "base input id is malformed", mutate: func(m *BaseImageMetadata) { m.BaseInputID = "nope" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			image := promoteImage()
			tc.mutate(&image)

			registry := &fakeBaseImageRegistry{}

			_, err := PromoteBaseImages(context.Background(), registry, &fakeBaseImageVerifier{}, io.Discard, promoteInputFor(image))
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			// The message names which entry failed. Only entry 0 is
			// checked here: the loop normalises and promotes one image
			// at a time, so reaching a later index means the earlier
			// ones were fully promoted first -- evidence verification
			// and its retries included -- which is more setup than this
			// assertion is worth.
			if !strings.Contains(err.Error(), "entry 0") {
				t.Errorf("err = %v, want it to name the entry", err)
			}
		})
	}
}
