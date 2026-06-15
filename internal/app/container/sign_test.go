// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

type recordingSigner struct {
	got cosign.SignImageInput
}

func (r *recordingSigner) SignImage(_ context.Context, in cosign.SignImageInput, _ io.Writer) error {
	r.got = in

	return nil
}

type recordingVerifier struct {
	got cosign.VerifyImageInput
}

func (r *recordingVerifier) VerifyImage(_ context.Context, in cosign.VerifyImageInput, _ io.Writer) error {
	r.got = in

	return nil
}

const testImageDigest = "ghcr.io/diggsweden/app@sha256:abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123abc1"

func TestSignImage_SigstoreDispatch(t *testing.T) {
	rec := &recordingSigner{}

	err := appcontainer.SignImage(context.Background(), rec, &bytes.Buffer{}, appcontainer.SignImageInput{
		Image:      testImageDigest,
		Method:     domainrelease.SignMethodSigstore,
		Recursive:  true,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	})
	if err != nil {
		t.Fatalf("SignImage: %v", err)
	}

	want := cosign.SignImageInput{
		ImageRef:   testImageDigest,
		Recursive:  true,
		Keyless:    true,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	}
	if rec.got != want {
		t.Errorf("SignImageInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

func TestSignImage_KMSDispatch(t *testing.T) {
	rec := &recordingSigner{}

	err := appcontainer.SignImage(context.Background(), rec, &bytes.Buffer{}, appcontainer.SignImageInput{
		Image:     testImageDigest,
		Method:    domainrelease.SignMethodKMS,
		Recursive: true,
		KeyRef:    "hashivault://transit/keys/release",
	})
	if err != nil {
		t.Fatalf("SignImage: %v", err)
	}

	want := cosign.SignImageInput{
		ImageRef:  testImageDigest,
		Recursive: true,
		KeyRef:    "hashivault://transit/keys/release",
	}
	if rec.got != want {
		t.Errorf("SignImageInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

func TestSignImage_GPGMethodRejected(t *testing.T) {
	err := appcontainer.SignImage(context.Background(), &recordingSigner{}, &bytes.Buffer{}, appcontainer.SignImageInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodGPG,
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("gpg cannot sign images; expected ErrInvalidConfig, got %v", err)
	}
}

func TestSignImage_EmptyImageRejected(t *testing.T) {
	err := appcontainer.SignImage(context.Background(), &recordingSigner{}, &bytes.Buffer{}, appcontainer.SignImageInput{
		Image:  "",
		Method: domainrelease.SignMethodSigstore,
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("empty image; expected ErrMissingInput, got %v", err)
	}
}

func TestVerifyImage_SigstoreDispatch(t *testing.T) {
	rec := &recordingVerifier{}

	err := appcontainer.VerifyImage(context.Background(), rec, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:              testImageDigest,
		Method:             domainrelease.SignMethodSigstore,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	})
	if err != nil {
		t.Fatalf("VerifyImage: %v", err)
	}

	want := cosign.VerifyImageInput{
		ImageRef:           testImageDigest,
		Keyless:            true,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	}
	if rec.got != want {
		t.Errorf("VerifyImageInput:\n got=%+v\nwant=%+v", rec.got, want)
	}
}

func TestVerifyImage_GPGMethodRejected(t *testing.T) {
	err := appcontainer.VerifyImage(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodGPG,
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("gpg cannot verify images; expected ErrInvalidConfig, got %v", err)
	}
}

func TestVerifyImage_MissingMethodRejected(t *testing.T) {
	err := appcontainer.VerifyImage(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:  testImageDigest,
		Method: "",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("missing --method; expected ErrMissingInput, got %v", err)
	}
}
