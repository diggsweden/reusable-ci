// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// recordingBlobber captures the SignBlobInput cosign would receive,
// without actually invoking cosign. Keeps the unit test free of the
// mockbinary stack — that's already covered at the adapter level.
type recordingBlobber struct {
	got cosign.SignBlobInput
}

func (r *recordingBlobber) SignBlob(_ context.Context, in cosign.SignBlobInput, _ io.Writer) error {
	r.got = in

	return nil
}

func TestNewCosignSigner_SigstoreRejectsKeyRef(t *testing.T) {
	_, err := apprelease.NewCosignSigner(&recordingBlobber{}, apprelease.CosignSignerInput{
		Method: domainrelease.SignMethodSigstore,
		KeyRef: "awskms:///alias/X",
	}, io.Discard)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("sigstore + KeyRef must reject as ErrUsage, got %v", err)
	}
}

func TestNewCosignSigner_RefusalDoesNotEchoProvidedValues(t *testing.T) {
	t.Parallel()

	const value = "private-reference-fixture"
	for _, tc := range []struct {
		field string
		in    apprelease.CosignSignerInput
	}{
		{"KeyRef", apprelease.CosignSignerInput{Method: domainrelease.SignMethodSigstore, KeyRef: value}},
		{"OIDCIssuer", apprelease.CosignSignerInput{Method: domainrelease.SignMethodKMS, KeyRef: "test-key", OIDCIssuer: value}},
		{"FulcioURL", apprelease.CosignSignerInput{Method: domainrelease.SignMethodKMS, KeyRef: "test-key", FulcioURL: value}},
		{"RekorURL", apprelease.CosignSignerInput{Method: domainrelease.SignMethodKMS, KeyRef: "test-key", RekorURL: value}},
		{"TrustedRootPath", apprelease.CosignSignerInput{Method: domainrelease.SignMethodKMS, KeyRef: "test-key", TrustedRootPath: value}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()

			signer, err := apprelease.NewCosignSigner(&recordingBlobber{}, tc.in, io.Discard)
			if !errors.Is(err, errs.ErrUsage) || signer != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}

			if !strings.Contains(err.Error(), tc.field) || strings.Contains(err.Error(), value) {
				t.Error("error must name the field without its value")
			}
		})
	}
}

func TestNewCosignSigner_KMSRequiresKeyRef(t *testing.T) {
	_, err := apprelease.NewCosignSigner(&recordingBlobber{}, apprelease.CosignSignerInput{
		Method: domainrelease.SignMethodKMS,
	}, io.Discard)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("kms without KeyRef must reject as ErrUsage, got %v", err)
	}
}

func TestNewCosignSigner_KMSRejectsOIDCIssuer(t *testing.T) {
	_, err := apprelease.NewCosignSigner(&recordingBlobber{}, apprelease.CosignSignerInput{
		Method:     domainrelease.SignMethodKMS,
		KeyRef:     "awskms:///alias/X",
		OIDCIssuer: "https://example",
	}, io.Discard)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("kms + OIDCIssuer must reject as ErrUsage, got %v", err)
	}
}

func TestNewCosignSigner_RejectsGPGMethod(t *testing.T) {
	_, err := apprelease.NewCosignSigner(&recordingBlobber{}, apprelease.CosignSignerInput{
		Method: domainrelease.SignMethodGPG,
	}, io.Discard)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("gpg method must reject as ErrUsage (wrong factory), got %v", err)
	}
}

func TestCosignSigner_SigstoreSignFileShape(t *testing.T) {
	rec := &recordingBlobber{}

	signer, err := apprelease.NewCosignSigner(rec, apprelease.CosignSignerInput{
		Method:     domainrelease.SignMethodSigstore,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), "app.tgz"); err != nil {
		t.Fatal(err)
	}

	want := cosign.SignBlobInput{
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	}
	if rec.got != want {
		t.Errorf("SignBlobInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

func TestCosignSigner_KMSSignFileShape(t *testing.T) {
	rec := &recordingBlobber{}

	signer, err := apprelease.NewCosignSigner(rec, apprelease.CosignSignerInput{
		Method: domainrelease.SignMethodKMS,
		KeyRef: "hashivault://transit/keys/release",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), "app.tgz"); err != nil {
		t.Fatal(err)
	}

	want := cosign.SignBlobInput{
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		KeyRef:     "hashivault://transit/keys/release",
	}
	if rec.got != want {
		t.Errorf("SignBlobInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

// TestCosignSigner_AdvertisesTheExtensionItWrites ties the two halves of the
// sidecar contract together. Extensions() is what the release flow later scans
// for to find a signature, and BundlePath is where cosign is told to put one.
// If those drift apart the signature is written under a name nothing looks
// for, and the release is published missing it.
//
// Both cosign methods write .bundle, so the previous table asserted the same
// value twice and could not have caught the two sides disagreeing.
func TestCosignSigner_AdvertisesTheExtensionItWrites(t *testing.T) {
	const artifact = "app.tgz"

	for name, in := range map[string]apprelease.CosignSignerInput{
		"sigstore": {Method: domainrelease.SignMethodSigstore, OIDCIssuer: "https://token.example.internal"},
		"kms":      {Method: domainrelease.SignMethodKMS, KeyRef: "awskms:///alias/X"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recordingBlobber{}

			signer, err := apprelease.NewCosignSigner(rec, in, io.Discard)
			if err != nil {
				t.Fatalf("method %s: %v", name, err)
			}

			want := []string{".bundle"}
			if got := signer.Extensions(); !slices.Equal(got, want) {
				t.Fatalf("extensions = %v, want %v", got, want)
			}

			if err := signer.SignFile(context.Background(), artifact); err != nil {
				t.Fatal(err)
			}

			if wantPath := artifact + signer.Extensions()[0]; rec.got.BundlePath != wantPath {
				t.Errorf("BundlePath = %q, but Extensions() advertises %q, so it wants %q",
					rec.got.BundlePath, signer.Extensions()[0], wantPath)
			}
		})
	}
}

// The endpoints must survive the whole way to the request, not merely be
// accepted by the constructor.
//
// This is the guard for a specific failure that happened: the flags were added
// to the CLI and the fields to the domain request, and nothing in between read
// them. The binary accepted --fulcio-url, documented it, and signed against
// public Sigstore anyway -- which an operator discovers from a certificate
// issued by the wrong authority, or not at all in the case that matters most,
// an air-gapped run that was supposed to contact nothing.
func TestCosignSigner_SigstoreCarriesSelfHostedEndpoints(t *testing.T) {
	rec := &recordingBlobber{}

	signer, err := apprelease.NewCosignSigner(rec, apprelease.CosignSignerInput{
		Method:          domainrelease.SignMethodSigstore,
		OIDCIssuer:      "https://gitlab.example.internal",
		FulcioURL:       "https://fulcio.example.internal",
		RekorURL:        "https://rekor.example.internal",
		TrustedRootPath: "trusted-root.json",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), "app.tgz"); err != nil {
		t.Fatal(err)
	}

	want := cosign.SignBlobInput{
		Artifact:        "app.tgz",
		BundlePath:      "app.tgz.bundle",
		Keyless:         true,
		OIDCIssuer:      "https://gitlab.example.internal",
		FulcioURL:       "https://fulcio.example.internal",
		RekorURL:        "https://rekor.example.internal",
		TrustedRootPath: "trusted-root.json",
	}
	if rec.got != want {
		t.Errorf("SignBlobInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

// KMS does not use keyless Fulcio or per-request Sigstore endpoint overrides;
// its separate adapter-level transparency policy controls public Rekor use.
func TestNewCosignSigner_KMSRejectsSigstoreEndpoints(t *testing.T) {
	for name, in := range map[string]apprelease.CosignSignerInput{
		"fulcio":       {Method: domainrelease.SignMethodKMS, KeyRef: "awskms://k", FulcioURL: "https://fulcio.example.internal"},
		"rekor":        {Method: domainrelease.SignMethodKMS, KeyRef: "awskms://k", RekorURL: "https://rekor.example.internal"},
		"trusted root": {Method: domainrelease.SignMethodKMS, KeyRef: "awskms://k", TrustedRootPath: "trusted-root-fixture.json"},
	} {
		t.Run(name, func(t *testing.T) {
			// ErrUsage, as every other refusal in this factory: naming a
			// Sigstore endpoint for a KMS signature is a mistake in the
			// command line, not a capability gap.
			if _, err := apprelease.NewCosignSigner(&recordingBlobber{}, in, io.Discard); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("kms with a %s URL: err = %v, want ErrUsage", name, err)
			}
		})
	}
}
