// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
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

	if err := signer.SignFile(context.Background(), "/tmp/app.tgz"); err != nil {
		t.Fatal(err)
	}

	want := cosign.SignBlobInput{
		Artefact:   "/tmp/app.tgz",
		BundlePath: "/tmp/app.tgz.bundle",
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

	if err := signer.SignFile(context.Background(), "/tmp/app.tgz"); err != nil {
		t.Fatal(err)
	}

	want := cosign.SignBlobInput{
		Artefact:   "/tmp/app.tgz",
		BundlePath: "/tmp/app.tgz.bundle",
		KeyRef:     "hashivault://transit/keys/release",
	}
	if rec.got != want {
		t.Errorf("SignBlobInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

func TestCosignSigner_ExtensionsMatchMethod(t *testing.T) {
	cases := []struct {
		method domainrelease.SignMethod
		want   []string
	}{
		{domainrelease.SignMethodSigstore, []string{".bundle"}},
		{domainrelease.SignMethodKMS, []string{".bundle"}},
	}

	for _, c := range cases {
		in := apprelease.CosignSignerInput{Method: c.method}
		if c.method == domainrelease.SignMethodKMS {
			in.KeyRef = "awskms:///alias/X"
		}

		signer, err := apprelease.NewCosignSigner(&recordingBlobber{}, in, io.Discard)
		if err != nil {
			t.Fatalf("method %s: %v", c.method, err)
		}

		got := signer.Extensions()
		if len(got) != len(c.want) {
			t.Errorf("method %s extensions = %v, want %v", c.method, got, c.want)

			continue
		}

		for i, ext := range c.want {
			if got[i] != ext {
				t.Errorf("method %s extensions[%d] = %q, want %q", c.method, i, got[i], ext)
			}
		}
	}
}
