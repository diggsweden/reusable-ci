// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package openpgp_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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

// armorPublicKeyRing serialises several entities into ONE armor block --
// the shape `gpg --armor --export A B` writes. Concatenating separate
// blocks instead reads back as one key only; that is a known behaviour,
// recorded in docs/open-questions.md ("Appending a key to
// allowed_gpg_keys.asc does not authorise it") and pinned at the tag
// level in app/validate/tags_allowlist_test.go.
func armorPublicKeyRing(t *testing.T, entities ...*gocrypto.Entity) []byte {
	t.Helper()

	var buf bytes.Buffer

	armorWriter, err := armor.Encode(&buf, gocrypto.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode public ring: %v", err)
	}

	for _, entity := range entities {
		if err := entity.Serialize(armorWriter); err != nil {
			t.Fatalf("Serialize public: %v", err)
		}
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

// TestVerifyDetachedArmored_RejectsASignatureFromAnotherKey is the
// identity half of verification. Every other GPG test in the repository
// signs and verifies with the same key, which a function that merely
// checked the signature was well-formed would also pass. This one signs
// with key A and offers key B as the trust anchor: only a check that
// binds signature to key can tell the difference.
func TestVerifyDetachedArmored_RejectsASignatureFromAnotherKey(t *testing.T) {
	t.Parallel()

	const message = "artifact bytes\n"

	signer := openpgp.NewSignerFromEntity(mintTestEntity(t))

	var sig bytes.Buffer
	if err := signer.WriteSignature(&sig, strings.NewReader(message)); err != nil {
		t.Fatal(err)
	}

	other := mintTestEntity(t)

	err := openpgp.VerifyDetachedArmored(
		strings.NewReader(message),
		bytes.NewReader(sig.Bytes()),
		armorPublicKey(t, other),
	)
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("a signature from an unrelated key verified against another key's armor: err = %v", err)
	}
}

// TestVerifyDetachedArmored_AcceptsAnyKeyInTheRing covers the allowlist
// shape: allowed_gpg_keys.asc is a bundle, and a signature from any key
// in it verifies. The signer is placed second so a check that only ever
// looked at the first entity would fail here.
func TestVerifyDetachedArmored_AcceptsAnyKeyInTheRing(t *testing.T) {
	t.Parallel()

	const message = "artifact bytes\n"

	entity := mintTestEntity(t)

	signer := openpgp.NewSignerFromEntity(entity)

	var sig bytes.Buffer
	if err := signer.WriteSignature(&sig, strings.NewReader(message)); err != nil {
		t.Fatal(err)
	}

	ring := armorPublicKeyRing(t, mintTestEntity(t), entity)

	if err := openpgp.VerifyDetachedArmored(strings.NewReader(message), bytes.NewReader(sig.Bytes()), ring); err != nil {
		t.Fatalf("signature from the second key in the ring rejected: %v", err)
	}
}

// TestVerifyDetachedArmored_RefusalsAreDistinguishable keeps a broken
// trust anchor from reading as a rejected signature: a caller that maps
// ErrPermissionDenied to "untrusted signer" must not be handed that for
// armor it simply failed to parse.
func TestVerifyDetachedArmored_RefusalsAreDistinguishable(t *testing.T) {
	t.Parallel()

	const message = "artifact bytes\n"

	entity := mintTestEntity(t)

	signer := openpgp.NewSignerFromEntity(entity)

	var sig bytes.Buffer
	if err := signer.WriteSignature(&sig, strings.NewReader(message)); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		pubKey  []byte
		sig     []byte
		message string
		want    error
	}{
		{
			name:    "unparseable public key armor",
			pubKey:  []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnot base64\n-----END PGP PUBLIC KEY BLOCK-----\n"),
			sig:     sig.Bytes(),
			message: message,
			want:    errs.ErrMalformedInput,
		},
		{
			name:    "no public key armor at all",
			pubKey:  nil,
			sig:     sig.Bytes(),
			message: message,
			want:    errs.ErrMalformedInput,
		},
		{
			// The artifact changed after signing: the same refusal as
			// an untrusted signer, which is what the caller reports.
			name:    "artifact does not match the signature",
			pubKey:  armorPublicKey(t, entity),
			sig:     sig.Bytes(),
			message: "artifact bytes tampered\n",
			want:    errs.ErrPermissionDenied,
		},
		{
			name:    "signature is not a signature",
			pubKey:  armorPublicKey(t, entity),
			sig:     []byte("-----BEGIN PGP SIGNATURE-----\nnot base64\n-----END PGP SIGNATURE-----\n"),
			message: message,
			want:    errs.ErrPermissionDenied,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := openpgp.VerifyDetachedArmored(strings.NewReader(tc.message), bytes.NewReader(tc.sig), tc.pubKey)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestPrimaryFingerprints_ReadsEveryKeyInTheBundle covers the other use
// of allowed_gpg_keys.asc: the same file is the trust anchor and the
// authorised-fingerprint list, so a bundle that verifies two signers has
// to yield two fingerprints. Dropping one would silently narrow the
// allowlist to whoever happens to be first.
func TestPrimaryFingerprints_ReadsEveryKeyInTheBundle(t *testing.T) {
	t.Parallel()

	first := mintTestEntity(t)
	second := mintTestEntity(t)

	got, err := openpgp.PrimaryFingerprints(armorPublicKeyRing(t, first, second))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		strings.ToUpper(hex.EncodeToString(first.PrimaryKey.Fingerprint)),
		strings.ToUpper(hex.EncodeToString(second.PrimaryKey.Fingerprint)),
	}

	if len(got) != len(want) {
		t.Fatalf("fingerprints = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fingerprint %d = %q, want %q", i, got[i], want[i])
		}

		if len(got[i]) != 40 {
			t.Errorf("fingerprint %d is %d characters, want the 40-char form gpg --with-colons prints", i, len(got[i]))
		}
	}
}

// TestPrimaryFingerprints_EmptyArmorIsNotAnError pins the documented
// "no keys from this source" contract, which is how a repository with no
// allowed_gpg_keys.asc is distinguished from one with a broken file.
func TestPrimaryFingerprints_EmptyArmorIsNotAnError(t *testing.T) {
	t.Parallel()

	got, err := openpgp.PrimaryFingerprints([]byte("  \n\t\n"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got (%v, %v), want (empty, nil)", got, err)
	}

	if _, err := openpgp.PrimaryFingerprints([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnope\n-----END PGP PUBLIC KEY BLOCK-----\n")); !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("a broken keyring file must not read as an empty allowlist: err = %v", err)
	}
}

// TestReadMetadata_ReportsTheIdentityAndKeyID covers the replacement for
// `gpg --list-secret-keys --with-colons`: the key ID is the fingerprint's
// last 16 characters, which is what the operator output and the forge
// signature UI are matched against.
func TestReadMetadata_ReportsTheIdentityAndKeyID(t *testing.T) {
	t.Parallel()

	entity := mintTestEntity(t)
	wantFP := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))

	for _, tc := range []struct {
		name  string
		armor []byte
	}{
		{name: "private key armor", armor: armorPrivateKey(t, entity, "")},
		{name: "public key armor", armor: armorPublicKey(t, entity)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			md, err := openpgp.ReadMetadata(tc.armor)
			if err != nil {
				t.Fatal(err)
			}

			if md.Fingerprint != wantFP {
				t.Errorf("fingerprint = %q, want %q", md.Fingerprint, wantFP)
			}

			if md.KeyID != wantFP[24:] {
				t.Errorf("key id = %q, want the last 16 of the fingerprint %q", md.KeyID, wantFP[24:])
			}

			if md.Name != "Test" || md.Email != "test@example.com" {
				t.Errorf("identity = (%q, %q), want (%q, %q)", md.Name, md.Email, "Test", "test@example.com")
			}
		})
	}
}

func TestReadMetadata_Refusals(t *testing.T) {
	t.Parallel()

	if _, err := openpgp.ReadMetadata([]byte(" \n")); !errors.Is(err, errs.ErrMissingInput) {
		t.Errorf("empty armor: err = %v, want ErrMissingInput", err)
	}

	if _, err := openpgp.ReadMetadata([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnope\n-----END PGP PUBLIC KEY BLOCK-----\n")); !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("malformed armor: err = %v, want ErrMalformedInput", err)
	}
}
