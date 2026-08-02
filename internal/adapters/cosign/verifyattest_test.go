// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestVerifyAttestation_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyAttestation(context.Background(), cosign.VerifyAttestationInput{
		ImageRef:           testImageDigest,
		PredicateType:      "slsaprovenance1",
		Keyless:            true,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}

	want := []string{
		"verify-attestation",
		"--type", "slsaprovenance1",
		"--certificate-identity-regexp", "^https://github.com/diggsweden/",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify-attestation argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestVerifyAttestation_KMSArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyAttestation(context.Background(), cosign.VerifyAttestationInput{
		ImageRef:      testImageDigest,
		PredicateType: "cyclonedx",
		KeyRef:        "./release-pubkey.pem",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}

	want := []string{
		"verify-attestation",
		"--type", "cyclonedx",
		"--key", "./release-pubkey.pem",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify-attestation argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestVerifyAttestation_RejectsMutableTag(t *testing.T) {
	a := &cosign.Adapter{}

	err := a.VerifyAttestation(context.Background(), cosign.VerifyAttestationInput{
		ImageRef:      "ghcr.io/diggsweden/app:latest",
		PredicateType: "slsaprovenance1",
		KeyRef:        "./k.pem",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (mutable tag must be refused)", err)
	}
}
