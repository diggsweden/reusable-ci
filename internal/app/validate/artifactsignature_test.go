// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// recordingVerifier captures the VerifyBlobInput the dispatcher
// builds, so we can pin the per-method args without a subprocess.
type recordingVerifier struct {
	got      cosign.VerifyBlobInput
	returnEr error
}

func (r *recordingVerifier) VerifyBlob(_ context.Context, in cosign.VerifyBlobInput, _ io.Writer) error {
	r.got = in

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

	w, err := armoredWriter(&pubArmor, "PGP PUBLIC KEY BLOCK")
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

	w, err := armoredWriter(&pubArmor, "PGP PUBLIC KEY BLOCK")
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
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("tampered artifact must yield ErrPermissionDenied, got %v", err)
	}
}

// armoredWriter returns the armored writer the test uses to
// serialise the public key (PGP PUBLIC KEY BLOCK).
func armoredWriter(w io.Writer, blockType string) (io.WriteCloser, error) {
	return armor.Encode(w, blockType, nil)
}

func TestVerifyArtifactSignature_ExplicitMethodOverridesDetection(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "app.tgz")
	touch(t, art)
	touch(t, art+".bundle")

	v := &recordingVerifier{returnEr: nil}

	// Auto-detect would inspect flags to choose between sigstore
	// and kms; setting --method=sigstore short-circuits that.
	err := appvalidate.VerifyArtifactSignature(context.Background(), v, &bytes.Buffer{}, appvalidate.ArtifactSignatureInput{
		Artifact:           art,
		Method:             domainrelease.SignMethodSigstore,
		CertIdentityRegexp: "^x",
		CertOIDCIssuer:     "https://x",
	})
	if err != nil {
		t.Fatalf("explicit method override: %v", err)
	}

	if !v.got.Keyless {
		t.Errorf("explicit sigstore method must dispatch to keyless verify; got %+v", v.got)
	}
}
