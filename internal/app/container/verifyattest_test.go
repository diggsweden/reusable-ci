// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

type recordingAttestationVerifier struct {
	got cosign.VerifyAttestationInput
}

func (r *recordingAttestationVerifier) VerifyAttestation(_ context.Context, in cosign.VerifyAttestationInput, _ io.Writer) error {
	r.got = in

	return nil
}

func TestVerifyAttestation_SigstoreDispatch(t *testing.T) {
	rec := &recordingAttestationVerifier{}

	err := appcontainer.VerifyAttestation(context.Background(), rec, &bytes.Buffer{}, appcontainer.VerifyAttestationInput{
		Image:              testImageDigest,
		Method:             domainrelease.SignMethodSigstore,
		PredicateType:      "slsaprovenance1",
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	})
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}

	want := cosign.VerifyAttestationInput{
		ImageRef:           testImageDigest,
		PredicateType:      "slsaprovenance1",
		Keyless:            true,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	}
	if rec.got != want {
		t.Errorf("VerifyAttestationInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

// TestVerifyAttestation_RejectsSLSAv02 keeps the verify side consistent with
// attest: only SLSA v1.0 (slsaprovenance1) is accepted; the obsolete v0.2 alias
// is refused so a verifier never silently checks the wrong predicate version.
func TestVerifyAttestation_RejectsSLSAv02(t *testing.T) {
	err := appcontainer.VerifyAttestation(context.Background(), &recordingAttestationVerifier{}, &bytes.Buffer{}, appcontainer.VerifyAttestationInput{
		Image:         testImageDigest,
		Method:        domainrelease.SignMethodSigstore,
		PredicateType: "slsaprovenance",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (v0.2 unsupported)", err)
	}
}

func TestVerifyAttestation_GPGMethodRejected(t *testing.T) {
	err := appcontainer.VerifyAttestation(context.Background(), &recordingAttestationVerifier{}, &bytes.Buffer{}, appcontainer.VerifyAttestationInput{
		Image:         testImageDigest,
		Method:        domainrelease.SignMethodGPG,
		PredicateType: "slsaprovenance1",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVerifyAttestation_MissingTypeRejected(t *testing.T) {
	err := appcontainer.VerifyAttestation(context.Background(), &recordingAttestationVerifier{}, &bytes.Buffer{}, appcontainer.VerifyAttestationInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodSigstore,
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}
}
