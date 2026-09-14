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

type recordingSigner struct {
	got   cosign.SignImageInput
	calls int
}

func (r *recordingSigner) SignImage(_ context.Context, in cosign.SignImageInput, _ io.Writer) error {
	r.got = in
	r.calls++

	return nil
}

type recordingVerifier struct {
	got       cosign.VerifyImageInput
	returnErr error
	calls     int
}

func (r *recordingVerifier) VerifyImage(_ context.Context, in cosign.VerifyImageInput, _ io.Writer) error {
	r.got = in
	r.calls++

	return r.returnErr
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

// TestVerifyImage_KMSDispatch is the counterpart to the sigstore dispatch.
// Signing covered both methods; verification covered only one, so the KMS
// branch -- which verifies against a key rather than a keyless identity -- had
// no test at all.
func TestVerifyImage_KMSDispatch(t *testing.T) {
	rec := &recordingVerifier{}

	err := appcontainer.VerifyImage(context.Background(), rec, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodKMS,
		KeyRef: "hashivault://transit/keys/release",
	})
	if err != nil {
		t.Fatalf("VerifyImage: %v", err)
	}

	// Keyless stays false: a KMS verification must not fall back to trusting
	// an OIDC identity.
	want := cosign.VerifyImageInput{
		ImageRef: testImageDigest,
		KeyRef:   "hashivault://transit/keys/release",
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

func TestVerifyImage_FailureIsValidation(t *testing.T) {
	t.Parallel()

	verifyErr := errSignatureMismatch

	err := appcontainer.VerifyImage(context.Background(), &recordingVerifier{returnErr: verifyErr}, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodKMS,
		KeyRef: "cosign.pub",
	})
	if !errors.Is(err, verifyErr) || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want verifier cause + ErrValidation", err)
	}

	if got := errs.ExitCodeFromError(err); got != errs.ExitCodeValidation {
		t.Errorf("exit code = %d, want validation (%d)", got, errs.ExitCodeValidation)
	}
}

func TestVerifyImage_MissingCosignRemainsDependencyUnavailable(t *testing.T) {
	t.Parallel()

	err := appcontainer.VerifyImage(context.Background(), &recordingVerifier{returnErr: errs.ErrDependencyUnavailable}, &bytes.Buffer{}, appcontainer.VerifyImageInput{
		Image:  testImageDigest,
		Method: domainrelease.SignMethodKMS,
		KeyRef: "cosign.pub",
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) || errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want only ErrDependencyUnavailable", err)
	}
}

// TestSignAndVerify_RefusalsReachNoCosign covers every input refusal on both
// verbs and requires each to leave cosign untouched.
//
// The refusal tests above pass a recording signer and never look at it, so a
// refusal that signed first and reported the error afterwards passed. That is
// not a hypothetical ordering mistake: the method switch is what refuses, and
// it sits after the fields have been read, so moving a check below the call is
// a one-line edit. Signing and then refusing publishes a signature for an image
// the command declined to sign, and a signature cannot be withdrawn — the
// registry has it, and so does the transparency log.
func TestSignAndVerify_RefusalsReachNoCosign(t *testing.T) {
	t.Parallel()

	t.Run("sign", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			in   appcontainer.SignImageInput
			want error
		}{
			{
				name: "empty image", in: appcontainer.SignImageInput{Method: domainrelease.SignMethodSigstore},
				want: errs.ErrMissingInput,
			},
			{
				name: "gpg cannot sign images", in: appcontainer.SignImageInput{
					Image: testImageDigest, Method: domainrelease.SignMethodGPG,
				},
				want: errs.ErrInvalidConfig,
			},
			{
				name: "an unknown method", in: appcontainer.SignImageInput{
					Image: testImageDigest, Method: domainrelease.SignMethod("magic"),
				},
				want: errs.ErrInvalidConfig,
			},
			{
				name: "no method at all", in: appcontainer.SignImageInput{Image: testImageDigest},
				want: errs.ErrInvalidConfig,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				rec := &recordingSigner{}

				err := appcontainer.SignImage(context.Background(), rec, &bytes.Buffer{}, tc.in)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}

				if rec.calls != 0 {
					t.Errorf("the refusal signed the image first (%d calls, request %+v)", rec.calls, rec.got)
				}
			})
		}
	})

	t.Run("verify", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			in   appcontainer.VerifyImageInput
			want error
		}{
			{
				name: "empty image", in: appcontainer.VerifyImageInput{Method: domainrelease.SignMethodKMS},
				want: errs.ErrMissingInput,
			},
			{
				name: "gpg cannot verify images", in: appcontainer.VerifyImageInput{
					Image: testImageDigest, Method: domainrelease.SignMethodGPG,
				},
				want: errs.ErrInvalidConfig,
			},
			{
				name: "no method at all", in: appcontainer.VerifyImageInput{Image: testImageDigest},
				want: errs.ErrMissingInput,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				rec := &recordingVerifier{}

				err := appcontainer.VerifyImage(context.Background(), rec, &bytes.Buffer{}, tc.in)
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}

				if rec.calls != 0 {
					t.Errorf("the refusal called the verifier first (%d calls, request %+v)", rec.calls, rec.got)
				}
			})
		}
	})
}

// TestSignAndVerify_RefuseWithoutAnAdapter is the nil-port half of the same
// rule, kept separate because the port types are unexported and a nil literal
// is the only way to express it from here.
func TestSignAndVerify_RefuseWithoutAnAdapter(t *testing.T) {
	t.Parallel()

	if err := appcontainer.SignImage(context.Background(), nil, &bytes.Buffer{},
		appcontainer.SignImageInput{Image: testImageDigest, Method: domainrelease.SignMethodSigstore},
	); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("sign without an adapter: err = %v, want ErrUsage", err)
	}

	if err := appcontainer.VerifyImage(context.Background(), nil, &bytes.Buffer{},
		appcontainer.VerifyImageInput{Image: testImageDigest, Method: domainrelease.SignMethodKMS, KeyRef: "cosign.pub"},
	); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("verify without an adapter: err = %v, want ErrUsage", err)
	}
}

// TestSignAndVerify_ForwardFieldsTheMethodDoesNotTake covers the fields each
// method refuses. They used to be dropped by a per-method request literal, so
// a Rekor URL or an OIDC issuer given with kms, or a key given with sigstore,
// signed against a service nobody named. They now reach the request, and its
// validation refuses them; the workflow input documents the issuer as
// forbidden for kms.
func TestSignAndVerify_ForwardFieldsTheMethodDoesNotTake(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]appcontainer.SignImageInput{
		"kms with a rekor url":    {Image: testImageDigest, Method: domainrelease.SignMethodKMS, KeyRef: "awskms:///alias/k", RekorURL: "https://rekor.example.internal"},
		"kms with an oidc issuer": {Image: testImageDigest, Method: domainrelease.SignMethodKMS, KeyRef: "awskms:///alias/k", OIDCIssuer: "https://issuer.example.internal"},
		"kms with a trusted root": {Image: testImageDigest, Method: domainrelease.SignMethodKMS, KeyRef: "awskms:///alias/k", TrustedRootPath: "root.json"},
		"sigstore with a key":     {Image: testImageDigest, Method: domainrelease.SignMethodSigstore, KeyRef: "awskms:///alias/k"},
	} {
		t.Run("sign "+name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingSigner{}
			if err := appcontainer.SignImage(context.Background(), rec, io.Discard, in); err != nil {
				t.Fatal(err)
			}

			want := cosign.SignImageInput{
				ImageRef: in.Image, Keyless: in.Method == domainrelease.SignMethodSigstore, KeyRef: in.KeyRef,
				OIDCIssuer: in.OIDCIssuer, RekorURL: in.RekorURL, TrustedRootPath: in.TrustedRootPath,
			}
			if rec.got != want {
				t.Errorf("request =\n%+v\nwant\n%+v", rec.got, want)
			}

			if err := rec.got.Validate(); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("request validation = %v, want ErrUsage", err)
			}
		})
	}

	for name, in := range map[string]appcontainer.VerifyImageInput{
		"kms with a certificate identity": {Image: testImageDigest, Method: domainrelease.SignMethodKMS, KeyRef: "k.pub", CertIdentityRegexp: "^https://github.com/o/r/"},
		"sigstore with a key":             {Image: testImageDigest, Method: domainrelease.SignMethodSigstore, KeyRef: "k.pub", CertIdentityRegexp: "^x$", CertOIDCIssuer: "https://issuer.example.internal"},
	} {
		t.Run("verify "+name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingVerifier{}

			err := appcontainer.VerifyImage(context.Background(), rec, io.Discard, in)
			if !errors.Is(err, errs.ErrUsage) || rec.calls != 0 {
				t.Errorf("err = %v, verifier calls = %d; want ErrUsage before cosign runs", err, rec.calls)
			}
		})
	}
}
