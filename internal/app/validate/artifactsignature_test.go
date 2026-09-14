// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// errVerifyFailed stands in for cosign refusing a signature.
var errVerifyFailed = errors.New("cosign: signature verification failed")

// recordingVerifier captures the VerifyBlobInput the dispatcher
// builds, so we can pin the per-method args without a subprocess.
//
// It keeps EVERY call rather than only the last, and keeps the context and
// error writer it was handed. A last-call-only recorder cannot tell one verify
// from two, and two verifications of the same bundle is not the same event as
// one: the second could carry different trust flags entirely.
type recordingVerifier struct {
	got      cosign.VerifyBlobInput
	all      []cosign.VerifyBlobInput
	gotCtx   context.Context //nolint:containedctx // recorded for identity assertions, never used to call anything.
	gotOut   io.Writer
	returnEr error
	calls    int
}

func (r *recordingVerifier) VerifyBlob(ctx context.Context, in cosign.VerifyBlobInput, out io.Writer) error {
	r.got = in
	r.all = append(r.all, in)
	r.gotCtx = ctx
	r.gotOut = out
	r.calls++

	return r.returnEr
}

func touch(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil { //nolint:gosec // test fixture.
		t.Fatalf("touch %s: %v", path, err)
	}
}

func TestVerifyArtifactSignature_DetectsKMSWhenBundleAndKeyRef(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".bundle")

	v := &recordingVerifier{}

	err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact: art,
		KeyRef:   "./pubkey.pem",
	})
	if err != nil {
		t.Fatalf("VerifyArtifactSignature: %v", err)
	}

	if v.got.Keyless {
		t.Errorf("KeyRef-driven dispatch must NOT trigger keyless verify; got %+v", v.got)
	}

	if v.got.KeyRef != "./pubkey.pem" {
		t.Errorf("KeyRef not propagated; got %q", v.got.KeyRef)
	}

	if v.got.BundlePath != art+".bundle" {
		t.Errorf("BundlePath = %q, want %s.bundle", v.got.BundlePath, art)
	}
}

func TestVerifyArtifactSignature_DetectsSigstoreWhenBundleAndIdentity(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".bundle")

	v := &recordingVerifier{}

	err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact:           art,
		CertIdentityRegexp: "^https://github.com/diggsweden/",
		CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
	})
	if err != nil {
		t.Fatalf("VerifyArtifactSignature: %v", err)
	}

	if !v.got.Keyless {
		t.Errorf("identity-driven dispatch must trigger keyless verify; got %+v", v.got)
	}

	if v.got.CertIdentityRegexp != "^https://github.com/diggsweden/" {
		t.Errorf("CertIdentityRegexp not propagated; got %q", v.got.CertIdentityRegexp)
	}

	// The issuer is the other half of the keyless trust anchor, and was
	// supplied by this test without ever being asserted. Dropping it
	// leaves cosign accepting a certificate from any issuer at all whose
	// subject happens to match the identity regexp.
	if v.got.CertOIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("CertOIDCIssuer not propagated; got %q", v.got.CertOIDCIssuer)
	}

	// A keyless verify must not also carry a key: that combination is
	// what the KMS branch is for, and cosign would take the key path.
	if v.got.KeyRef != "" {
		t.Errorf("keyless verify carried a KeyRef: %q", v.got.KeyRef)
	}
}

func TestVerifyArtifactSignature_CosignFailureIsValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   appvalidate.ArtifactSignatureInput
	}{
		{
			name: "keyless",
			in:   appvalidate.ArtifactSignatureInput{CertIdentityRegexp: "^https://example/", CertOIDCIssuer: "https://issuer"},
		},
		{
			name: "kms",
			in:   appvalidate.ArtifactSignatureInput{KeyRef: "./pubkey.pem"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			art := filepath.Join(dir, "app.tgz")
			touch(t, art)
			touch(t, art+".bundle")

			in := tc.in
			in.Artifact = art

			v := &recordingVerifier{returnEr: errVerifyFailed}

			err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, in)

			// The cause is preserved, which is right.
			if !errors.Is(err, errVerifyFailed) {
				t.Fatalf("err = %v, want it to wrap the verifier error", err)
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("cosign verify failure must carry ErrValidation: %v", err)
			}

			if got := errs.ExitCodeFromError(err); got != errs.ExitCodeValidation {
				t.Errorf("exit code = %d, want validation (%d)", got, errs.ExitCodeValidation)
			}
		})
	}
}

func TestVerifyArtifactSignature_BundleWithoutIdentityFlags(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".bundle")

	err := appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact: art,
		// neither --key nor --cert-identity-regexp supplied → reject
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("bundle without identity constraint must reject as ErrMissingInput, got %v", err)
	}
}

func TestVerifyArtifactSignature_DualSidecarsRejectsWithoutMethod(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".bundle")
	touch(t, art+".asc")

	err := appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact: art,
	})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Errorf("dual-sign state without --method must reject as ErrInvalidConfig, got %v", err)
	}
}

func TestVerifyArtifactSignature_NoSidecarsErrors(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)

	err := appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact: art,
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("no sidecars must reject as ErrMissingInput, got %v", err)
	}
}

func TestVerifyArtifactSignature_GPGRequiresPublicKey(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".asc")

	err := appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact: art,
		// PublicKey missing
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("gpg verify without --public-key must reject as ErrMissingInput, got %v", err)
	}
}

func TestVerifyArtifactSignature_GPGRoundtripVerifies(t *testing.T) {
	// End-to-end GPG: generate an entity, sign a payload, verify
	// using only the public half. Validates the openpgp.Verify-
	// DetachedArmored path the dispatcher delegates to.
	entity, err := gocrypto.NewEntity("Test", "", "test@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")

	payload := []byte("hello world")
	if err = os.WriteFile(art, payload, 0o644); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	// Produce a detached signature into <art>.asc.
	sigPath := art + ".asc"

	sigFile, err := os.Create(sigPath) //nolint:gosec // test fixture; sigPath is t.TempDir-based.
	if err != nil {
		t.Fatal(err)
	}

	if err = gocrypto.ArmoredDetachSign(sigFile, entity, bytes.NewReader(payload), nil); err != nil {
		t.Fatal(err)
	}

	if err = sigFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Serialise the public half for the verifier.
	var pubArmor bytes.Buffer

	w, err := armor.Encode(&pubArmor, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = entity.Serialize(w); err != nil {
		t.Fatal(err)
	}

	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	err = appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact:  art,
		PublicKey: pubArmor.Bytes(),
	})
	if err != nil {
		t.Errorf("expected GPG verify to succeed, got %v", err)
	}
}

func TestVerifyArtifactSignature_GPGTamperedArtifactFails(t *testing.T) {
	entity, err := gocrypto.NewEntity("Test", "", "test@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	sigPath := art + ".asc"

	if err = os.WriteFile(art, []byte("original"), 0o644); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	sigFile, err := os.Create(sigPath) //nolint:gosec // test fixture.
	if err != nil {
		t.Fatal(err)
	}

	if err = gocrypto.ArmoredDetachSign(sigFile, entity, bytes.NewReader([]byte("original")), nil); err != nil {
		t.Fatal(err)
	}

	if err = sigFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Tamper: overwrite artifact with different content; sig is now
	// for a hash that doesn't match.
	if err = os.WriteFile(art, []byte("tampered"), 0o644); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	var pubArmor bytes.Buffer

	w, err := armor.Encode(&pubArmor, "PGP PUBLIC KEY BLOCK", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = entity.Serialize(w); err != nil {
		t.Fatal(err)
	}

	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	err = appvalidate.VerifyArtifactSignature(context.Background(), &recordingVerifier{}, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact:  art,
		PublicKey: pubArmor.Bytes(),
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("tampered artifact must yield ErrValidation, got %v", err)
	}

	if got := errs.ExitCodeFromError(err); got != errs.ExitCodeValidation {
		t.Errorf("exit code = %d, want validation (%d)", got, errs.ExitCodeValidation)
	}
}

func TestVerifyArtifactSignature_ExplicitMethodOverridesDetection(t *testing.T) {
	for _, layout := range []string{"asc", "dual", "explicit path"} {
		t.Run(layout, func(t *testing.T) {
			dir := t.TempDir()
			art := filepath.Join(dir, "app.tgz")
			touch(t, art)

			// Detection would pick gpg from a lone .asc, and refuse dual sidecars.
			if layout != "explicit path" {
				touch(t, art+".asc")
			}

			if layout == "dual" {
				touch(t, art+".bundle")
			}

			signature := ""
			if layout == "explicit path" {
				signature = filepath.Join(dir, "selected.bundle")
				touch(t, signature)
			}

			v := &recordingVerifier{returnEr: nil}

			err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
				Artifact:           art,
				Method:             domainrelease.SignMethodSigstore,
				SignaturePath:      signature,
				CertIdentityRegexp: "^x",
				CertOIDCIssuer:     "https://x",
			})
			if err != nil {
				t.Fatalf("explicit method override: %v", err)
			}

			if signature == "" {
				signature = art + ".bundle"
			}

			want := domainrelease.BlobVerifyRequest{Artifact: art, BundlePath: signature, Keyless: true, CertIdentityRegexp: "^x", CertOIDCIssuer: "https://x"}
			if v.calls != 1 || !reflect.DeepEqual(v.got, want) {
				t.Fatalf("calls=%d request=%+v want=%+v", v.calls, v.got, want)
			}
		})
	}
}

// TestVerifyArtifactSignature_ContradictoryTrustInputsAreRefused covers the
// trust inputs a method cannot honour. Each would once have been dropped on the
// way to cosign, so the verify passed on a constraint the operator never got:
// a pinned key ignored by a keyless verify, or an identity ignored by a KMS one.
// Every case is otherwise valid, and none may reach the verifier.
func TestVerifyArtifactSignature_ContradictoryTrustInputsAreRefused(t *testing.T) {
	const (
		identity = "^https://github.com/diggsweden/"
		issuer   = "https://token.actions.githubusercontent.com"
		key      = "./pubkey.pem"
	)

	for _, tc := range []struct {
		name string
		in   appvalidate.ArtifactSignatureInput
		want error
	}{
		{"sigstore with a key", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodSigstore, CertIdentityRegexp: identity, CertOIDCIssuer: issuer, KeyRef: key}, errs.ErrUsage},
		{"sigstore without an identity", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodSigstore, CertOIDCIssuer: issuer}, errs.ErrUsage},
		{"sigstore without an issuer", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodSigstore, CertIdentityRegexp: identity}, errs.ErrUsage},
		{"kms without a key", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodKMS}, errs.ErrUsage},
		{"kms with an identity", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodKMS, KeyRef: key, CertIdentityRegexp: identity}, errs.ErrUsage},
		{"kms with an issuer", appvalidate.ArtifactSignatureInput{Method: domainrelease.SignMethodKMS, KeyRef: key, CertOIDCIssuer: issuer}, errs.ErrUsage},
		{"detected kms with an issuer", appvalidate.ArtifactSignatureInput{KeyRef: key, CertOIDCIssuer: issuer}, errs.ErrUsage},
		{"unsupported method", appvalidate.ArtifactSignatureInput{Method: "x509", KeyRef: key}, errs.ErrInvalidConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			art := filepath.Join(dir, "app.tgz")
			touch(t, art)
			touch(t, art+".bundle")

			in := tc.in
			in.Artifact = art

			v := &recordingVerifier{}

			err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if errors.Is(err, errs.ErrValidation) {
				t.Errorf("a refused request is not a signature mismatch: %v", err)
			}

			if v.calls != 0 {
				t.Errorf("verifier called %d times for a refused request: %+v", v.calls, v.all)
			}
		})
	}
}

// TestVerifyArtifactSignature_InfrastructureFailureIsNotAMismatch keeps a
// missing cosign or a cancelled job apart from a signature that failed to
// verify: only the last one says anything about the artifact.
func TestVerifyArtifactSignature_InfrastructureFailureIsNotAMismatch(t *testing.T) {
	for _, cause := range []error{errs.ErrDependencyUnavailable, context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			dir := t.TempDir()
			art := filepath.Join(dir, "app.tgz")
			touch(t, art)
			touch(t, art+".bundle")

			v := &recordingVerifier{returnEr: fmt.Errorf("run cosign: %w", cause)}

			err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{Artifact: art, KeyRef: "./pubkey.pem"})
			if !errors.Is(err, cause) || errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want %v without ErrValidation", err, cause)
			}

			if v.calls != 1 {
				t.Errorf("verifier called %d times, want 1", v.calls)
			}
		})
	}
}

// ctxKey types a value used only to recognise the caller's context again.
type ctxKey struct{}

// TestVerifyArtifactSignature_SendsTheCompleteRequestForEachMethod compares the
// whole request, for both cosign methods, against an expected value.
//
// The per-method tests above each check three or four fields of the recorded
// request and leave the rest unread — including Artifact, which is the file the
// signature is being checked against. Pointing the sigstore branch at a
// different existing file left every one of them passing: the trust flags were
// still right, they were just being applied to the wrong artifact. That is the
// failure this command exists to prevent, and it was the one field nobody
// looked at.
//
// Comparing the struct whole removes the choice. A field added later is covered
// the day it is added, rather than the day someone remembers to assert it.
func TestVerifyArtifactSignature_SendsTheCompleteRequestForEachMethod(t *testing.T) {
	dir := t.TempDir()

	artifact := filepath.Join(dir, "app.tgz")
	touch(t, artifact)
	touch(t, artifact+".bundle")

	// A second signed artifact sitting next to the first, so "verified the
	// wrong file" is a reachable mistake rather than a hypothetical one.
	decoy := filepath.Join(dir, "other.tgz")
	touch(t, decoy)
	touch(t, decoy+".bundle")

	for _, tc := range []struct {
		name string
		in   appvalidate.ArtifactSignatureInput
		want cosign.VerifyBlobInput
	}{
		{
			name: "keyless",
			in: appvalidate.ArtifactSignatureInput{
				Artifact:           artifact,
				CertIdentityRegexp: "^https://github.com/diggsweden/",
				CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
			},
			want: cosign.VerifyBlobInput{
				Artifact:           artifact,
				BundlePath:         artifact + ".bundle",
				Keyless:            true,
				CertIdentityRegexp: "^https://github.com/diggsweden/",
				CertOIDCIssuer:     "https://token.actions.githubusercontent.com",
			},
		},
		{
			name: "kms",
			in: appvalidate.ArtifactSignatureInput{
				Artifact: artifact,
				KeyRef:   "./pubkey.pem",
			},
			want: cosign.VerifyBlobInput{
				Artifact:   artifact,
				BundlePath: artifact + ".bundle",
				KeyRef:     "./pubkey.pem",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier := &recordingVerifier{}

			ctx := context.WithValue(context.Background(), ctxKey{}, "caller")

			var out bytes.Buffer

			if err := appvalidate.VerifyArtifactSignature(ctx, verifier, &out, tc.in); err != nil {
				t.Fatalf("VerifyArtifactSignature: %v", err)
			}

			if verifier.calls != 1 {
				t.Fatalf("VerifyBlob called %d times, want exactly 1; requests = %+v", verifier.calls, verifier.all)
			}

			if verifier.all[0] != tc.want {
				t.Errorf("request = %+v\nwant     %+v", verifier.all[0], tc.want)
			}

			// The caller's context must reach cosign, or a cancelled release
			// job would keep a verification running past its deadline.
			if verifier.gotCtx == nil || verifier.gotCtx.Value(ctxKey{}) != "caller" {
				t.Errorf("VerifyBlob got a context that is not the caller's: %v", verifier.gotCtx)
			}

			// And the caller's writer, so cosign's diagnostics land where the
			// command is writing rather than being dropped.
			if verifier.gotOut != io.Writer(&out) {
				t.Errorf("VerifyBlob got writer %T, want the caller's *bytes.Buffer", verifier.gotOut)
			}
		})
	}
}
