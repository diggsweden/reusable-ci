// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/mockbinary"
)

func TestSignBlob_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	}, nil)
	if err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	invs := bins.Invocations("cosign")
	if len(invs) != 1 {
		t.Fatalf("expected 1 cosign invocation, got %d", len(invs))
	}

	want := []string{
		"sign-blob", "--yes",
		"--bundle", "app.tgz.bundle",
		"--oidc-issuer", "https://token.actions.githubusercontent.com",
		"app.tgz",
	}
	if !slices.Equal(invs[0].Args, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", invs[0].Args, want)
	}
}

func TestSignBlob_KeylessNoIssuerOmitsFlag(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, nil); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	got := bins.Invocations("cosign")[0].Args
	if slices.Contains(got, "--oidc-issuer") {
		t.Errorf("empty OIDCIssuer must NOT produce --oidc-issuer flag; argv=%v", got)
	}
}

func TestSignBlob_KMSArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		KeyRef:     "hashivault://transit/keys/release",
	}, nil)
	if err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	want := []string{
		"sign-blob", "--yes",
		"--bundle", "app.tgz.bundle",
		"--key", "hashivault://transit/keys/release",
		"app.tgz",
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestSignBlob_RejectsKeylessWithKey(t *testing.T) {
	a := cosign.New()

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
		KeyRef:     "awskms:///alias/X",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("keyless + KeyRef should be rejected as ErrUsage, got %v", err)
	}
}

func TestSignBlob_RejectsNonKeylessWithoutKey(t *testing.T) {
	a := cosign.New()

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("non-keyless without KeyRef should be rejected as ErrUsage, got %v", err)
	}
}

func TestSignBlob_RejectsIssuerInKMSMode(t *testing.T) {
	a := cosign.New()

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		OIDCIssuer: "https://example",
		KeyRef:     "awskms:///alias/X",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("KMS + OIDCIssuer should be rejected as ErrUsage, got %v", err)
	}
}

func TestSignBlob_PropagatesCosignFailure(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", "exit 1")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, nil)
	if err == nil {
		t.Fatal("expected non-nil error when cosign exits 1")
	}

	if !strings.Contains(err.Error(), "cosign") {
		t.Errorf("error must mention cosign for diagnosability; got %v", err)
	}
}

func TestSignBlob_RedactsKeyMaterialFromStderr(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", `cat <<EOF >&2
-----BEGIN OPENSSH PRIVATE KEY-----
NEVER-SHOULD-LEAK
-----END OPENSSH PRIVATE KEY-----
EOF
exit 1`)

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	var captured bytes.Buffer

	_ = a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, &captured)

	if strings.Contains(captured.String(), "NEVER-SHOULD-LEAK") {
		t.Errorf("redactor missed PEM private-key block; captured:\n%s", captured.String())
	}

	if !strings.Contains(captured.String(), "redacted") {
		t.Errorf("redaction notice missing; captured:\n%s", captured.String())
	}
}

func TestVerifyBlob_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyBlob(context.Background(), cosign.VerifyBlobInput{
		Artefact:           "app.tgz",
		BundlePath:         "app.tgz.bundle",
		Keyless:            true,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyBlob: %v", err)
	}

	want := []string{
		"verify-blob",
		"--bundle", "app.tgz.bundle",
		"--new-bundle-format",
		"--certificate-identity-regexp", "^https://github.com/diggsweden/",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		"app.tgz",
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestVerifyBlob_KMSArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyBlob(context.Background(), cosign.VerifyBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		KeyRef:     "./release-pubkey.pem",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyBlob: %v", err)
	}

	want := []string{
		"verify-blob",
		"--bundle", "app.tgz.bundle",
		"--new-bundle-format",
		"--key", "./release-pubkey.pem",
		"app.tgz",
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify argv:\n got=%v\nwant=%v", got, want)
	}
}

const testImageDigest = "ghcr.io/diggsweden/app@sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestSignImage_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef:   testImageDigest,
		Recursive:  true,
		Keyless:    true,
		OIDCIssuer: "https://token.actions.githubusercontent.com",
	}, nil)
	if err != nil {
		t.Fatalf("SignImage: %v", err)
	}

	want := []string{
		"sign", "--yes", "--recursive",
		"--oidc-issuer", "https://token.actions.githubusercontent.com",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestSignImage_KMSArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef:  testImageDigest,
		Recursive: true,
		KeyRef:    "hashivault://transit/keys/release",
	}, nil)
	if err != nil {
		t.Fatalf("SignImage: %v", err)
	}

	want := []string{
		"sign", "--yes", "--recursive",
		"--key", "hashivault://transit/keys/release",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestSignImage_NonRecursiveOmitsFlag(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef: testImageDigest,
		KeyRef:   "awskms:///alias/X",
	}, nil); err != nil {
		t.Fatalf("SignImage: %v", err)
	}

	got := bins.Invocations("cosign")[0].Args
	if slices.Contains(got, "--recursive") {
		t.Errorf("Recursive=false must NOT add --recursive; argv=%v", got)
	}
}

func TestSignImage_RejectsMutableTag(t *testing.T) {
	a := cosign.New()

	err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef: "ghcr.io/diggsweden/app:latest", // tag, not digest
		Keyless:  true,
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("mutable tag must be rejected as ErrUsage (cosign refuses to sign mutable tags), got %v", err)
	}
}

func TestSignImage_RejectsKeylessWithKey(t *testing.T) {
	a := cosign.New()

	err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef: testImageDigest,
		Keyless:  true,
		KeyRef:   "awskms:///alias/X",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("keyless + KeyRef should be rejected as ErrUsage, got %v", err)
	}
}

func TestSignImage_RejectsNonKeylessWithoutKey(t *testing.T) {
	a := cosign.New()

	err := a.SignImage(context.Background(), cosign.SignImageInput{
		ImageRef: testImageDigest,
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("non-keyless without KeyRef should be rejected as ErrUsage, got %v", err)
	}
}

func TestVerifyImage_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyImage(context.Background(), cosign.VerifyImageInput{
		ImageRef:           testImageDigest,
		Keyless:            true,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyImage: %v", err)
	}

	want := []string{
		"verify",
		"--certificate-identity-regexp", "^https://github.com/diggsweden/",
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestVerifyImage_KMSArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.VerifyImage(context.Background(), cosign.VerifyImageInput{
		ImageRef: testImageDigest,
		KeyRef:   "./release-pubkey.pem",
	}, nil)
	if err != nil {
		t.Fatalf("VerifyImage: %v", err)
	}

	want := []string{
		"verify",
		"--key", "./release-pubkey.pem",
		testImageDigest,
	}

	got := bins.Invocations("cosign")[0].Args
	if !slices.Equal(got, want) {
		t.Errorf("verify argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestVerifyImage_RejectsMutableTag(t *testing.T) {
	a := cosign.New()

	err := a.VerifyImage(context.Background(), cosign.VerifyImageInput{
		ImageRef: "ghcr.io/diggsweden/app:v1.0.0",
		KeyRef:   "./pubkey.pem",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("mutable tag must be rejected (verify must pin to digest), got %v", err)
	}
}

func TestVerifyBlob_RejectsKeylessWithoutIdentity(t *testing.T) {
	a := cosign.New()

	err := a.VerifyBlob(context.Background(), cosign.VerifyBlobInput{
		Artefact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("keyless verify without identity constraints must reject as ErrUsage, got %v", err)
	}
}
