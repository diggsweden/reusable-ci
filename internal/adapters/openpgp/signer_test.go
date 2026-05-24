// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package openpgp_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/diggsweden/reusable-ci/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// mintTestEntity creates a fresh OpenPGP entity in-process. Replaces the
// gpgkey/ test harness that previously shelled out to `gpg --batch
// --gen-key`. No GNUPGHOME, no subprocess, no temp files.
func mintTestEntity(t *testing.T) *gocrypto.Entity {
	t.Helper()

	entity, err := gocrypto.NewEntity("Test", "ci", "test@example.com", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}

	return entity
}

// armorPrivateKey serialises a freshly-minted entity to ASCII armor,
// optionally encrypting the private key material with passphrase. The
// returned blob is what a workflow's secret store would contain.
func armorPrivateKey(t *testing.T, entity *gocrypto.Entity, passphrase string) []byte {
	t.Helper()

	if passphrase != "" {
		if err := entity.EncryptPrivateKeys([]byte(passphrase), nil); err != nil {
			t.Fatalf("EncryptPrivateKeys: %v", err)
		}
	}

	var buf bytes.Buffer

	armorWriter, err := armor.Encode(&buf, gocrypto.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}

	if err := entity.SerializePrivateWithoutSigning(armorWriter, nil); err != nil {
		t.Fatalf("SerializePrivate: %v", err)
	}

	if err := armorWriter.Close(); err != nil {
		t.Fatalf("armor close: %v", err)
	}

	return buf.Bytes()
}

// armorPublicKey serialises the public half so a test verifier can
// confirm the detached signature.
func armorPublicKey(t *testing.T, entity *gocrypto.Entity) []byte {
	t.Helper()

	var buf bytes.Buffer

	armorWriter, err := armor.Encode(&buf, gocrypto.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode public: %v", err)
	}

	if err := entity.Serialize(armorWriter); err != nil {
		t.Fatalf("Serialize public: %v", err)
	}

	if err := armorWriter.Close(); err != nil {
		t.Fatalf("armor close: %v", err)
	}

	return buf.Bytes()
}

func TestNewSignerFromArmor_AcceptsUnencryptedKey(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "")

	signer, err := openpgp.NewSignerFromArmor(armor, "")
	if err != nil {
		t.Fatalf("NewSignerFromArmor: %v", err)
	}

	if signer.Fingerprint() == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestNewSignerFromArmor_AcceptsEncryptedKeyWithPassphrase(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)

	armor := armorPrivateKey(t, entity, "swordfish")
	if _, err := openpgp.NewSignerFromArmor(armor, "swordfish"); err != nil {
		t.Fatalf("NewSignerFromArmor: %v", err)
	}
}

func TestNewSignerFromArmor_RejectsWrongPassphrase(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "swordfish")

	_, err := openpgp.NewSignerFromArmor(armor, "wrong")
	if err == nil || !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestNewSignerFromArmor_RejectsEncryptedKeyWithoutPassphrase(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "swordfish")

	_, err := openpgp.NewSignerFromArmor(armor, "")
	if err == nil || !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestNewSignerFromArmor_RejectsEmptyArmor(t *testing.T) {
	t.Parallel()

	for _, armor := range [][]byte{nil, []byte("   \n\n  ")} {
		_, err := openpgp.NewSignerFromArmor(armor, "")
		if err == nil || !errors.Is(err, errs.ErrMissingInput) {
			t.Errorf("err = %v, want ErrMissingInput", err)
		}
	}
}

func TestNewSignerFromArmor_RejectsMalformedArmor(t *testing.T) {
	t.Parallel()

	_, err := openpgp.NewSignerFromArmor([]byte("not a key"), "")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestNewSignerFromArmor_RejectsPublicKey(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	pub := armorPublicKey(t, entity)

	_, err := openpgp.NewSignerFromArmor(pub, "")
	if err == nil || !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput (private half missing)", err)
	}
}

func TestSignFile_ProducesVerifiableSignature(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	signer := openpgp.NewSignerFromEntity(entity)

	dir := t.TempDir()

	artifact := filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(artifact, []byte("hello world"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), artifact); err != nil {
		t.Fatalf("SignFile: %v", err)
	}

	// The signature must verify against the entity's public half.
	sigBody, err := os.ReadFile(artifact + ".asc") //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(sigBody), "-----BEGIN PGP SIGNATURE-----") {
		t.Errorf("missing ASCII-armor header in signature: %s", sigBody)
	}

	// Verify via go-crypto.
	pubKeyRing := gocrypto.EntityList{entity}

	signedReader, err := os.Open(artifact) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = signedReader.Close() }()

	if _, err := gocrypto.CheckArmoredDetachedSignature(pubKeyRing, signedReader, bytes.NewReader(sigBody), nil); err != nil {
		t.Errorf("signature failed verification: %v", err)
	}
}

func TestSignFile_TruncatesExistingAsc(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	signer := openpgp.NewSignerFromEntity(entity)
	dir := t.TempDir()

	artifact := filepath.Join(dir, "x")
	if err := os.WriteFile(artifact, []byte("body"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	// Pre-stage a stale .asc with junk content — re-signing must overwrite.
	if err := os.WriteFile(artifact+".asc", []byte("STALE"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), artifact); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(artifact + ".asc") //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(body), "STALE") {
		t.Error(".asc still carries stale content; must be truncated on re-sign")
	}
}

func TestSignFile_RejectsUninitialisedSigner(t *testing.T) {
	t.Parallel()

	var s *openpgp.Signer

	err := s.SignFile(context.Background(), "anything")
	if err == nil || !strings.Contains(err.Error(), "not initialised") {
		t.Errorf("err = %v, want 'not initialised'", err)
	}
}

func TestWriteSignature_StreamsToWriter(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	signer := openpgp.NewSignerFromEntity(entity)

	var sigBuf bytes.Buffer
	if err := signer.WriteSignature(&sigBuf, strings.NewReader("payload")); err != nil {
		t.Fatalf("WriteSignature: %v", err)
	}

	if _, err := gocrypto.CheckArmoredDetachedSignature(gocrypto.EntityList{entity}, strings.NewReader("payload"), &sigBuf, nil); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

func TestFingerprint_StableAcrossReloadFromArmor(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "")

	first, err := openpgp.NewSignerFromArmor(armor, "")
	if err != nil {
		t.Fatal(err)
	}

	second, err := openpgp.NewSignerFromArmor(armor, "")
	if err != nil {
		t.Fatal(err)
	}

	if first.Fingerprint() != second.Fingerprint() {
		t.Errorf("fingerprints differ: %q vs %q", first.Fingerprint(), second.Fingerprint())
	}

	if len(first.Fingerprint()) != 40 {
		t.Errorf("fingerprint length = %d, want 40 (legacy v4 RSA)", len(first.Fingerprint()))
	}
}

var _ = io.EOF
