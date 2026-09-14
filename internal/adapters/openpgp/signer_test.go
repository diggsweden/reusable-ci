// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package openpgp_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pgp"
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

// armorPublicKeyRing serialises several entities into one armor block,
// the shape `gpg --armor --export A B` writes.
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

func TestNewSignerFromArmor_EncryptedKeySignsWithPassphrase(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"primary", "signing subkey"} {
		t.Run(key, func(t *testing.T) {
			entity := mintTestEntity(t)
			if key == "signing subkey" {
				require.NoError(t, entity.AddSigningSubkey(nil))
			}

			pubKeyRing, err := gocrypto.ReadArmoredKeyRing(bytes.NewReader(armorPublicKey(t, entity)))
			require.NoError(t, err)
			keyArmor := armorPrivateKey(t, entity, "swordfish")
			createdAt := time.Now().Truncate(time.Second).Add(time.Second)
			verifyConfig := &packet.Config{Time: func() time.Time { return createdAt.Add(time.Hour) }}

			for _, tc := range []struct {
				name string
				new  func() (*openpgp.Signer, error)
			}{
				{"Armor", func() (*openpgp.Signer, error) {
					return openpgp.NewSignerFromArmor(keyArmor, "swordfish")
				}},
				{"ArmorAt", func() (*openpgp.Signer, error) {
					return openpgp.NewSignerFromArmorAt(keyArmor, "swordfish", createdAt)
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					signer, err := tc.new()
					require.NoError(t, err)

					const payload = "encrypted-key payload\x00\r\n"

					var sig bytes.Buffer
					require.NoError(t, signer.WriteSignature(&sig, strings.NewReader(payload)), "WriteSignature")
					_, err = gocrypto.CheckArmoredDetachedSignature(pubKeyRing, strings.NewReader(payload), &sig, verifyConfig)
					require.NoError(t, err, "writer signature does not verify")

					artifact := filepath.Join(t.TempDir(), "artifact.bin")
					require.NoError(t, os.WriteFile(artifact, []byte(payload), 0o600))
					require.NoError(t, signer.SignFile(context.Background(), artifact), "SignFile")
					body, err := os.ReadFile(artifact + ".asc")
					require.NoError(t, err)
					_, err = gocrypto.CheckArmoredDetachedSignature(pubKeyRing, strings.NewReader(payload), bytes.NewReader(body), verifyConfig)
					require.NoError(t, err, "file signature does not verify")
				})
			}
		})
	}
}

func TestNewSignerFromArmor_RejectsWrongPassphrase(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "swordfish")

	_, err := openpgp.NewSignerFromArmor(armor, "wrong")
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestNewSignerFromArmor_RejectsEncryptedKeyWithoutPassphrase(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	armor := armorPrivateKey(t, entity, "swordfish")

	_, err := openpgp.NewSignerFromArmor(armor, "")
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestNewSignerFromArmor_RejectsEmptyArmor(t *testing.T) {
	t.Parallel()

	for _, armor := range [][]byte{nil, []byte("   \n\n  ")} {
		_, err := openpgp.NewSignerFromArmor(armor, "")
		if !errors.Is(err, errs.ErrMissingInput) {
			t.Errorf("err = %v, want ErrMissingInput", err)
		}
	}
}

func TestNewSignerFromArmor_RejectsMalformedArmor(t *testing.T) {
	t.Parallel()

	_, err := openpgp.NewSignerFromArmor([]byte("not a key"), "")
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput — a broken key file must not read as a missing one", err)
	}
}

func TestNewSignerFromArmor_RejectsPublicKey(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	pub := armorPublicKey(t, entity)

	_, err := openpgp.NewSignerFromArmor(pub, "")
	if !errors.Is(err, errs.ErrMissingInput) {
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

func TestSignFile_RefusesUnsafeSidecarWithoutChangingFiles(t *testing.T) {
	t.Parallel()

	signer := openpgp.NewSignerFromEntity(mintTestEntity(t))
	for _, tc := range []struct {
		name        string
		parentLink  bool
		wantEntries int
	}{{"sidecar link", false, 2}, {"parent link", true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			parentLink := tc.parentLink
			root, outside := t.TempDir(), t.TempDir()

			artifact := filepath.Join(root, "artifact")
			if err := os.WriteFile(artifact, []byte("artifact bytes"), 0o600); err != nil {
				t.Fatal(err)
			}

			canary := filepath.Join(outside, "canary")
			if err := os.WriteFile(canary, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}

			if parentLink {
				link := filepath.Join(outside, "linked")
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}

				artifact = filepath.Join(link, "artifact")
			} else if err := os.Symlink(canary, artifact+".asc"); err != nil {
				t.Fatal(err)
			}

			if err := signer.SignFile(context.Background(), artifact); !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}

			body, err := os.ReadFile(canary)
			if err != nil || string(body) != "unchanged" {
				t.Errorf("canary changed: %v", err)
			}

			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}

			if len(entries) != tc.wantEntries {
				t.Errorf("unexpected sidecar or staging files: %v", entries)
			}
		})
	}
}

func TestSignFile_ReadFailurePreservesExistingSidecar(t *testing.T) {
	t.Parallel()
	signer := openpgp.NewSignerFromEntity(mintTestEntity(t))

	artifact := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(artifact, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(artifact+".asc", []byte("old signature"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := signer.SignFile(context.Background(), artifact); err == nil {
		t.Error("directory read succeeded")
	}

	body, err := os.ReadFile(artifact + ".asc")
	if err != nil || string(body) != "old signature" {
		t.Errorf("old signature changed: %v", err)
	}
}

func TestSignFile_ReplacesStaleSidecarWithExactSignature(t *testing.T) {
	t.Parallel()
	entity := mintTestEntity(t)
	createdAt := entity.PrimaryKey.CreationTime.Add(time.Second)
	signer, err := openpgp.NewSignerFromEntityAt(entity, createdAt)
	require.NoError(t, err)
	pubKeyRing, err := gocrypto.ReadArmoredKeyRing(bytes.NewReader(armorPublicKey(t, entity)))
	require.NoError(t, err)

	verifyConfig := &packet.Config{Time: func() time.Time { return createdAt.Add(time.Hour) }}

	const payload = "artifact bytes\x00\r\n"

	var want bytes.Buffer
	require.NoError(t, signer.WriteSignature(&want, strings.NewReader(payload)))
	dir := t.TempDir()

	artifact := filepath.Join(dir, "x")
	require.NoError(t, os.WriteFile(artifact, []byte(payload), 0o600))
	// The old sidecar is longer than the replacement, exposing non-truncating writes.
	require.NoError(t, os.WriteFile(artifact+".asc", bytes.Repeat([]byte("STALE"), want.Len()+1), 0o600))

	require.NoError(t, signer.SignFile(context.Background(), artifact))

	body, err := os.ReadFile(artifact + ".asc") //nolint:gosec // test fixture
	require.NoError(t, err)

	_, err = gocrypto.CheckArmoredDetachedSignature(pubKeyRing, strings.NewReader(payload), bytes.NewReader(body), verifyConfig)
	require.NoError(t, err, "replacement signature does not verify for the exact artifact")
	// Verification alone can ignore trailing bytes after the armor block.
	if !bytes.Equal(body, want.Bytes()) {
		t.Errorf("replacement differs from the complete signature: got %d bytes, want %d", len(body), want.Len())
	}

	_, err = gocrypto.CheckArmoredDetachedSignature(pubKeyRing, strings.NewReader(payload+"tampered"), bytes.NewReader(body), verifyConfig)
	require.Error(t, err, "replacement signature verifies for a different artifact")

	gotArtifact, err := os.ReadFile(artifact)
	if err != nil || string(gotArtifact) != payload {
		t.Errorf("artifact changed: body = %q, err = %v", gotArtifact, err)
	}
}

func TestSignFile_RejectsUninitialisedSigner(t *testing.T) {
	t.Parallel()

	var s *openpgp.Signer

	err := s.SignFile(context.Background(), "anything")
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "not initialised") {
		t.Errorf("err = %v, want ErrUsage naming the uninitialised signer", err)
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

func TestSigner_FixedCreationTimeMatchesRequestAndIsByteStable(t *testing.T) {
	t.Parallel()

	entity := mintTestEntity(t)
	keyArmor := armorPrivateKey(t, entity, "")
	pubKeyRing, err := gocrypto.ReadArmoredKeyRing(bytes.NewReader(armorPublicKey(t, entity)))
	require.NoError(t, err)

	createdAt := entity.PrimaryKey.CreationTime.Add(24 * time.Hour)
	// Verify after the requested epoch; packet decoding below checks its exact value.
	verifyConfig := &packet.Config{Time: func() time.Time { return createdAt.Add(time.Hour) }}

	for _, tc := range []struct {
		name string
		new  func() (*openpgp.Signer, error)
	}{
		{"EntityAt", func() (*openpgp.Signer, error) {
			return openpgp.NewSignerFromEntityAt(entity, createdAt)
		}},
		{"ArmorAt", func() (*openpgp.Signer, error) {
			return openpgp.NewSignerFromArmorAt(keyArmor, "", createdAt)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signer, err := tc.new()
			require.NoError(t, err)

			var first, second bytes.Buffer
			require.NoError(t, signer.WriteSignature(&first, strings.NewReader("payload")))
			require.NoError(t, signer.WriteSignature(&second, strings.NewReader("payload")))
			artifact := filepath.Join(t.TempDir(), "artifact")
			require.NoError(t, os.WriteFile(artifact, []byte("payload"), 0o600))
			require.NoError(t, signer.SignFile(context.Background(), artifact))
			fileSig, err := os.ReadFile(artifact + ".asc")
			require.NoError(t, err)

			for _, output := range []struct {
				name string
				body []byte
			}{{"writer", first.Bytes()}, {"writer repeat", second.Bytes()}, {"file", fileSig}} {
				t.Run(output.name, func(t *testing.T) {
					if !bytes.Equal(first.Bytes(), output.body) {
						t.Error("fixed-time OpenPGP signatures differ")
					}

					_, verifyErr := gocrypto.CheckArmoredDetachedSignature(pubKeyRing, strings.NewReader("payload"), bytes.NewReader(output.body), verifyConfig)
					require.NoError(t, verifyErr, "signature does not verify")

					block, err := armor.Decode(bytes.NewReader(output.body))
					require.NoError(t, err)
					decoded, err := packet.Read(block.Body)
					require.NoError(t, err)

					sig, ok := decoded.(*packet.Signature)
					require.True(t, ok, "decoded packet = %T, want signature", decoded)

					if !sig.CreationTime.Equal(createdAt) {
						t.Errorf("signature creation time = %s, want requested %s", sig.CreationTime, createdAt)
					}
				})
			}
		})
	}
}

func TestNewSignerFromEntityAt_RejectsV6Key(t *testing.T) {
	t.Parallel()

	entity, err := gocrypto.NewEntity("Test", "ci", "test@example.com", &packet.Config{V6Keys: true})
	if err != nil {
		t.Fatal(err)
	}

	_, err = openpgp.NewSignerFromEntityAt(entity, entity.PrimaryKey.CreationTime.Add(time.Second))
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestFingerprint_UppercasePrimaryFingerprintAcrossReloadFromArmor(t *testing.T) {
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

	want := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))
	for i, signer := range []*openpgp.Signer{first, second} {
		if got := signer.Fingerprint(); got != want {
			t.Errorf("reload %d fingerprint = %q, want uppercase primary fingerprint %q", i, got, want)
		}
	}
}

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

	err := pgp.VerifyDetachedArmored(
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

	if err := pgp.VerifyDetachedArmored(strings.NewReader(message), bytes.NewReader(sig.Bytes()), ring); err != nil {
		t.Fatalf("signature from the second key in the ring rejected: %v", err)
	}
}

// TestVerifyDetachedArmored_AcceptsAKeyInAnAppendedBlock covers the
// verification half of the concatenated-bundle shape.
//
// The allowlist and the trust anchor are the same file, read by two
// functions. When only PrimaryFingerprints learned to read every armor
// block, a key added with `>>` would be authorised and then refused as
// "signature made by unknown entity" before the fingerprint check ran.
// Both readers have to see the whole file or neither should.
func TestVerifyDetachedArmored_AcceptsAKeyInAnAppendedBlock(t *testing.T) {
	t.Parallel()

	const message = "artifact bytes\n"

	entity := mintTestEntity(t)

	signer := openpgp.NewSignerFromEntity(entity)

	var sig bytes.Buffer
	if err := signer.WriteSignature(&sig, strings.NewReader(message)); err != nil {
		t.Fatal(err)
	}

	// Two separate armor blocks, as `gpg --armor --export >> file`
	// writes them -- not one block holding two keys.
	outgoing := armorPublicKey(t, mintTestEntity(t))
	incoming := armorPublicKey(t, entity)
	appended := append(append([]byte{}, outgoing...), incoming...)

	if err := pgp.VerifyDetachedArmored(strings.NewReader(message), bytes.NewReader(sig.Bytes()), appended); err != nil {
		t.Fatalf("a signature from a key in the appended block was rejected: %v", err)
	}
}

// TestPrimaryFingerprints_ReadsEveryConcatenatedBlock is the allowlist
// half of the same file shape: every key the operator committed has to
// reach the authorised set, or the allowlist is narrower than the file
// with only a key count to say so.
func TestPrimaryFingerprints_ReadsEveryConcatenatedBlock(t *testing.T) {
	t.Parallel()

	first := mintTestEntity(t)
	second := mintTestEntity(t)

	appended := append(append([]byte{}, armorPublicKey(t, first)...), armorPublicKey(t, second)...)

	got, err := pgp.PrimaryFingerprints(appended)
	if err != nil {
		t.Fatalf("PrimaryFingerprints: %v", err)
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
			name:    "unparsable public key armor",
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

			err := pgp.VerifyDetachedArmored(strings.NewReader(tc.message), bytes.NewReader(tc.sig), tc.pubKey)
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

	got, err := pgp.PrimaryFingerprints(armorPublicKeyRing(t, first, second))
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

	got, err := pgp.PrimaryFingerprints([]byte("  \n\t\n"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got (%v, %v), want (empty, nil)", got, err)
	}

	if _, err := pgp.PrimaryFingerprints([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnope\n-----END PGP PUBLIC KEY BLOCK-----\n")); !errors.Is(err, errs.ErrMalformedInput) {
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
