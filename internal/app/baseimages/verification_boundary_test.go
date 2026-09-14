// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestBaseVerificationBoundary_OnlyTypedAbsenceIsMissing(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{nil, errs.ErrMissingInput, errs.ErrPermissionDenied, errs.ErrDependencyUnavailable, context.Canceled, context.DeadlineExceeded} {
		in := BaseImageVerifyExistingInput{Flavors: []string{"rust", "go"}, BaseInputID: promoteInput, ExpectedRepository: promoteRepo, ExpectedSource: "https://source.example/repo", ExpectedWorkflow: "base.yml", CosignPublicKey: "fixture.pub"}
		first, second := promoteRepo+":"+promoteInput+"-rust", promoteRepo+":"+promoteInput+"-go"
		registry := &fakeBaseImageRegistry{digests: map[string]string{first: promoteDigest, second: promoteDigest}, resolveErr: map[string]error{second: cause}}
		verifier := &boundaryVerifier{fakeBaseImageVerifier: &fakeBaseImageVerifier{payload: baseLineagePayload(t,
			lineageFields{source: in.ExpectedSource, workflow: in.ExpectedWorkflow, flavor: "rust", baseInputID: promoteInput},
			lineageFields{source: in.ExpectedSource, workflow: in.ExpectedWorkflow, flavor: "go", baseInputID: promoteInput})}}

		var out bytes.Buffer

		result, err := VerifyExistingBaseImages(t.Context(), registry, verifier, &out, in)

		switch {
		case cause == nil:
			if err != nil || !result.AllFound || len(verifier.refs) != 2 || !strings.Contains(out.String(), "already exists and verifies") {
				t.Fatalf("err=%v result=%+v verify=%v out=%s", err, result, verifier.refs, &out)
			}
		case errors.Is(cause, errs.ErrMissingInput):
			if err != nil || result.AllFound || result.MissingFlavorsJSON != `["go"]` || len(verifier.refs) != 1 {
				t.Fatalf("missing result=%+v err=%v", result, err)
			}
		default:
			if !errors.Is(err, cause) || len(verifier.refs) != 0 || out.Len() != 0 {
				t.Fatalf("cause=%v err=%v verifier=%v out=%s", cause, err, verifier.refs, &out)
			}
		}
	}
}

func TestBaseVerificationBoundary_AllFlavorsAndIDsPreflight(t *testing.T) {
	t.Parallel()

	for _, change := range []func(*BaseImageVerifyExistingInput){
		func(in *BaseImageVerifyExistingInput) { in.Flavors = []string{"rust", "bad/flavor"} },
		func(in *BaseImageVerifyExistingInput) { in.Flavors = []string{"rust", "rust"} },
		func(in *BaseImageVerifyExistingInput) {
			in.BaseInputID = ""
			in.BaseInputs = []BaseInput{{Flavor: "rust", BaseInputID: promoteInput}}
		},
		func(in *BaseImageVerifyExistingInput) {
			in.BaseInputs = []BaseInput{{Flavor: "rust", BaseInputID: promoteInput}, {Flavor: "go", BaseInputID: "bad"}}
		},
		func(in *BaseImageVerifyExistingInput) {
			in.BaseInputs = []BaseInput{{Flavor: "rust", BaseInputID: promoteInput}, {Flavor: "unused/invalid", BaseInputID: promoteInput}}
		},
		func(in *BaseImageVerifyExistingInput) {
			in.BaseInputs = []BaseInput{{Flavor: "rust", BaseInputID: promoteInput}, {Flavor: "rust", BaseInputID: promoteInput}}
		},
	} {
		in := BaseImageVerifyExistingInput{Flavors: []string{"rust", "go"}, BaseInputID: promoteInput, ExpectedRepository: promoteRepo, ExpectedSource: "https://source.example/repo", ExpectedWorkflow: "base.yml", CosignPublicKey: "fixture.pub"}
		change(&in)

		registry := &fakeBaseImageRegistry{digests: map[string]string{promoteRepo + ":" + promoteInput + "-rust": promoteDigest}}
		verifier := &boundaryVerifier{fakeBaseImageVerifier: &fakeBaseImageVerifier{payload: baseLineagePayload(t, lineageFields{source: in.ExpectedSource, workflow: in.ExpectedWorkflow, flavor: "rust", baseInputID: promoteInput})}}

		var out bytes.Buffer

		_, err := VerifyExistingBaseImages(t.Context(), registry, verifier, &out, in)
		if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "entry 1") || len(registry.resolved) != 0 || len(verifier.refs) != 0 || out.Len() != 0 {
			t.Fatalf("err=%v resolved=%v verifier=%v out=%s", err, registry.resolved, verifier.refs, &out)
		}
	}
}

func TestCandidatePairBoundary_RejectsIncompleteLaterEntry(t *testing.T) {
	t.Parallel()

	for _, refOnly := range []bool{true, false} {
		first := promoteImage()
		second := first
		second.Flavor = "go"

		second.Tag = promoteRepo + ":go-final"
		if refOnly {
			second.CandidateRef = second.Ref
		} else {
			second.CandidateTag = promoteRepo + ":candidate-go"
		}

		in := promoteInputFor(first, second)
		registry := &fakeBaseImageRegistry{digests: map[string]string{first.Tag: promoteDigest, second.Tag: promoteDigest, promoteRepo + ":candidate-go": promoteDigest}}
		verifier := &boundaryVerifier{fakeBaseImageVerifier: &fakeBaseImageVerifier{payload: baseLineagePayload(t, lineageFields{source: in.ExpectedSource, workflow: in.ExpectedWorkflow, flavor: "rust", baseInputID: promoteInput}, lineageFields{source: in.ExpectedSource, workflow: in.ExpectedWorkflow, flavor: "go", baseInputID: promoteInput})}}

		var out bytes.Buffer

		_, err := PromoteBaseImages(t.Context(), registry, verifier, &out, in)
		if !errors.Is(err, errs.ErrValidation) || len(registry.resolved) != 0 || len(registry.copied) != 0 || len(verifier.refs) != 0 || out.Len() != 0 {
			t.Fatalf("err=%v resolved=%v copies=%v out=%s", err, registry.resolved, registry.copied, &out)
		}
	}
}
