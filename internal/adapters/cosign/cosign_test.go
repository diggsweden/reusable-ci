// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestSignBlob_KeylessArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact:   "app.tgz",
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

	// The issuer is named in the signing config, not on argv: cosign 3.x
	// deprecated --oidc-issuer and refuses it alongside a config.
	want := []string{
		"sign-blob", "--yes",
		"--bundle", "app.tgz.bundle",
		"--signing-config", invs[0].Args[slices.Index(invs[0].Args, "--signing-config")+1],
		"app.tgz",
	}
	if !slices.Equal(invs[0].Args, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", invs[0].Args, want)
	}
}

func TestCopyImage_ArgvShape(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.CopyImage(context.Background(), cosign.CopyImageInput{
		Source: "codeberg.org/o/r:staging-v1.2.3",
		Dest:   "ghcr.io/o/r:release",
	}, nil)
	if err != nil {
		t.Fatalf("CopyImage: %v", err)
	}

	want := []string{"copy", "--force", "codeberg.org/o/r:staging-v1.2.3", "ghcr.io/o/r:release"}
	if got := bins.Invocations("cosign")[0].Args; !slices.Equal(got, want) {
		t.Errorf("argv:\n got=%v\nwant=%v", got, want)
	}
}

func TestCopyImage_RejectsEmptyRefs(t *testing.T) {
	a := cosign.New()

	if err := a.CopyImage(context.Background(), cosign.CopyImageInput{Dest: "ghcr.io/o/r:release"}, nil); err == nil {
		t.Error("empty source must be rejected")
	}

	if err := a.CopyImage(context.Background(), cosign.CopyImageInput{Source: "codeberg.org/o/r:v1"}, nil); err == nil {
		t.Error("empty dest must be rejected")
	}
}

func TestSignBlob_KeylessNoIssuerOmitsFlag(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact:   "app.tgz",
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
		Artifact:   "app.tgz",
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
		Artifact:   "app.tgz",
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
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("non-keyless without KeyRef should be rejected as ErrUsage, got %v", err)
	}
}

func TestSignBlob_RejectsIssuerInKMSMode(t *testing.T) {
	a := cosign.New()

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact:   "app.tgz",
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
		Artifact:   "app.tgz",
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
		Artifact:   "app.tgz",
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
		Artifact:           "app.tgz",
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
		Artifact:   "app.tgz",
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

	got := bins.Invocations("cosign")[0].Args

	want := []string{
		"sign", "--yes",
		"--signing-config", got[slices.Index(got, "--signing-config")+1],
		"--recursive",
		testImageDigest,
	}

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
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, nil)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("keyless verify without identity constraints must reject as ErrUsage, got %v", err)
	}
}

// A self-hosted Sigstore is reached through the signing config, which is the
// only channel cosign 3.x still listens on.
//
// This test previously asserted --fulcio-url and --oidc-issuer on the argv. Both
// are deprecated in cosign 3.x and, worse, refused alongside a signing config --
// which this adapter already passes whenever the transparency log is off. So the
// combination a self-hosted deployment actually runs failed outright, and the
// test was pinning the broken shape.
func TestSignBlob_KeylessSelfHostedSigstoreArgvShape(t *testing.T) {
	captured := filepath.Join(t.TempDir(), "signing-config.json")

	bins := mockbinary.New(t)
	// The adapter removes the document when the call returns, which is correct
	// and makes it unreadable afterwards -- so the fake cosign keeps a copy
	// while it exists, the same way a real one would read it.
	bins.Add("cosign", `while [ $# -gt 0 ]; do
  if [ "$1" = "--signing-config" ]; then cp "$2" `+captured+`; fi
  shift
done`)

	a := &cosign.Adapter{Bin: bins.Path("cosign")}

	err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
		OIDCIssuer: "https://gitlab.example.internal",
		FulcioURL:  "https://fulcio.example.internal",
	}, nil)
	if err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	got := bins.Invocations("cosign")[0].Args

	// The deprecated flags must not appear at all: cosign rejects the run
	// outright when they accompany a config.
	for _, deprecated := range []string{"--fulcio-url", "--rekor-url", "--oidc-issuer"} {
		if slices.Contains(got, deprecated) {
			t.Errorf("argv carries %s, which cosign 3.x refuses alongside --signing-config: %v", deprecated, got)
		}
	}

	if !slices.Contains(got, "--signing-config") {
		t.Fatalf("no --signing-config in argv: %v", got)
	}

	// The document is the assertion, not the flag: a config naming none of the
	// services would satisfy the flag and send the run to public Sigstore.
	document, readErr := os.ReadFile(captured)
	if readErr != nil {
		t.Fatalf("read signing config: %v", readErr)
	}

	for _, want := range []string{"https://fulcio.example.internal", "https://gitlab.example.internal"} {
		if !strings.Contains(string(document), want) {
			t.Errorf("signing config does not name %s:\n%s", want, document)
		}
	}
}

// With every service at its default and the log on, cosign's own configuration
// is correct, so the run must carry no config at all -- passing one could only
// diverge from what cosign would have done.
func TestSignBlob_KeylessWithoutEndpointsOmitsThem(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("cosign", ":")

	a := &cosign.Adapter{Bin: bins.Path("cosign"), Transparency: domainrelease.TransparencyPublic}

	if err := a.SignBlob(context.Background(), cosign.SignBlobInput{
		Artifact:   "app.tgz",
		BundlePath: "app.tgz.bundle",
		Keyless:    true,
	}, nil); err != nil {
		t.Fatalf("SignBlob: %v", err)
	}

	got := bins.Invocations("cosign")[0].Args
	for _, flag := range []string{"--fulcio-url", "--rekor-url", "--oidc-issuer", "--signing-config"} {
		if slices.Contains(got, flag) {
			t.Errorf("argv carries %s with nothing configured: %v", flag, got)
		}
	}
}
