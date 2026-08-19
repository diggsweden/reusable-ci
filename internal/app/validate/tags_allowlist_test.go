// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	adapteropenpgp "github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

const gpgSignedTagBody = "object abc\ntype commit\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n"

// armoredPublicKey mints a fresh entity and returns its ASCII-armored
// public key plus the uppercase-hex primary-key fingerprint — the shape
// a committed .reusable-ci/allowed_gpg_keys.asc would carry.
func armoredPublicKey(t *testing.T) ([]byte, string) {
	t.Helper()

	entity, err := gocrypto.NewEntity("Allowed Signer", "ci", "signer@example.com", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}

	var buf bytes.Buffer

	writer, err := armor.Encode(&buf, gocrypto.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}

	if err = entity.Serialize(writer); err != nil {
		t.Fatalf("Serialize public key: %v", err)
	}

	_ = writer.Close()

	fps, err := adapteropenpgp.PrimaryFingerprints(buf.Bytes())
	if err != nil || len(fps) != 1 {
		t.Fatalf("PrimaryFingerprints: %v (n=%d)", err, len(fps))
	}

	return buf.Bytes(), fps[0]
}

// armoredPublicKeyRing returns one armor block holding n public keys --
// the shape `gpg --armor --export A B` writes -- with their fingerprints
// in the same order.
func armoredPublicKeyRing(t *testing.T, n int) ([]byte, []string) {
	t.Helper()

	var buf bytes.Buffer

	writer, err := armor.Encode(&buf, gocrypto.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode: %v", err)
	}

	for i := range n {
		entity, entityErr := gocrypto.NewEntity(fmt.Sprintf("Allowed Signer %d", i), "ci", "signer@example.com", nil)
		if entityErr != nil {
			t.Fatalf("NewEntity: %v", entityErr)
		}

		if err = entity.Serialize(writer); err != nil {
			t.Fatalf("Serialize public key: %v", err)
		}
	}

	_ = writer.Close()

	fps, err := adapteropenpgp.PrimaryFingerprints(buf.Bytes())
	if err != nil || len(fps) != n {
		t.Fatalf("PrimaryFingerprints: %v (n=%d, want %d)", err, len(fps), n)
	}

	return buf.Bytes(), fps
}

// writeGPGKeysFile writes body as the project's allowed_gpg_keys.asc and
// returns its path.
func writeGPGKeysFile(t *testing.T, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "allowed_gpg_keys.asc")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// C2: no allowlist at all + require → fail closed.
func TestTagSignature_GPG_NoAllowlistRequireFailsClosed(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: true, verifySigFingerprint: "AAAA"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       filepath.Join(t.TempDir(), "missing.asc"),
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

// B: no allowlist + require=false → loud warning, pass.
func TestTagSignature_GPG_NoAllowlistNoRequireWarns(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: true, verifySigFingerprint: "AAAA"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                "v1.0.0",
		AllowedGPGKeysPath: filepath.Join(t.TempDir(), "missing.asc"),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	for _, want := range []string{"::warning title=No release signer allowlist::", "⚠️ No GPG signer allowlist"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

// Keys file present + verified signer NOT in the bundle → denied
// (the allowlist exists, so it is authoritative).
func TestTagSignature_GPG_SignerNotInKeysFileDenied(t *testing.T) {
	t.Parallel()

	keyArmor, _ := armoredPublicKey(t) // bundle authorises this key's fingerprint…
	// …but the tag verified to a different fingerprint.
	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: true, verifySigFingerprint: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                "v1.0.0",
		AllowedGPGKeysPath: writeGPGKeysFile(t, keyArmor),
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

// C2: keys file present but signature unverifiable + require → fail closed.
func TestTagSignature_GPG_UnverifiableRequireFailsClosed(t *testing.T) {
	t.Parallel()

	keyArmor, _ := armoredPublicKey(t)
	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: false}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, keyArmor),
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

// C1: allowed_gpg_keys.asc is both verification keyring and allowlist.
func TestTagSignature_GPG_KeysFileDerivesAllowlist(t *testing.T) {
	t.Parallel()

	keyArmor, fpr := armoredPublicKey(t)
	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: true, verifySigFingerprint: fpr, verifySigSigner: "Allowed Signer <signer@example.com>"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, keyArmor),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if !strings.Contains(out.String(), "Signer fingerprint is authorised") {
		t.Errorf("missing authorised note:\n%s", out.String())
	}

	// The committed key must have been handed to the verifier as keyring
	// material — that's what closes the verifiability gap.
	if !bytes.Contains(gitr.verifySigArmor, bytes.TrimSpace(keyArmor)) {
		t.Errorf("committed key was not passed to VerifyTagSignature as keyring material")
	}
}

// TestTagSignature_GPG_AllowlistHonoursEveryKeyInABlock covers key
// rotation, which is why the bundle holds more than one key at a time:
// the incoming key is committed alongside the outgoing one, and tags
// signed by either must be authorised until the old one is removed.
//
// Every other allowlist fixture here holds exactly one key.
//
// This uses the shape `gpg --armor --export A B` produces: one armor
// block containing both keys. The shape the documentation actually tells
// operators to create does not work -- see the test below.
func TestTagSignature_GPG_AllowlistHonoursEveryKeyInABlock(t *testing.T) {
	t.Parallel()

	bundle, fingerprints := armoredPublicKeyRing(t, 2)

	gitr := &fakeTagGit{
		body:                 gpgSignedTagBody,
		verifySigOK:          true,
		verifySigFingerprint: fingerprints[1], // signed by the incoming key
		verifySigSigner:      "Allowed Signer <signer@example.com>",
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, bundle),
	})
	if err != nil {
		t.Fatalf("second key in the bundle was not authorised: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "Signer fingerprint is authorised") {
		t.Errorf("missing authorised note:\n%s", out.String())
	}
}

// TestTagSignature_GPG_ConcatenatedKeyBlocksLoseAllButTheFirst records a
// defect, not a guarantee.
//
// docs/verification.md describes allowed_gpg_keys.asc as "one or more PGP
// PUBLIC KEY BLOCK sections concatenated" and tells operators to add a
// key with `gpg --armor --export <email> >> .reusable-ci/allowed_gpg_keys.asc`.
// That append produces a second armor block, and only the first block is
// read -- so a signer added by following the documented instructions is
// denied, with no error and no warning about the half-read file.
//
// It fails closed, so this denies a legitimate signer rather than
// admitting an unauthorised one. See docs/open-questions.md.
func TestTagSignature_GPG_ConcatenatedKeyBlocksLoseAllButTheFirst(t *testing.T) {
	t.Parallel()

	outgoing, _ := armoredPublicKey(t)
	incoming, incomingFPR := armoredPublicKey(t)

	// Exactly what `>>` appends produce.
	concatenated := append(append([]byte{}, outgoing...), incoming...)

	gitr := &fakeTagGit{
		body:                 gpgSignedTagBody,
		verifySigOK:          true,
		verifySigFingerprint: incomingFPR,
		verifySigSigner:      "Allowed Signer <signer@example.com>",
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, concatenated),
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied (the appended key is not seen)", err)
	}

	// The count in the message is the only hint that the file was read
	// only in part.
	if !strings.Contains(err.Error(), "1 key(s)") {
		t.Errorf("error should show how many keys were parsed: %v", err)
	}
}

// SSH: no allowed_signers + require → fail closed.
func TestTagSignature_SSH_NoAllowlistRequireFailsClosed(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedSignersPath:       filepath.Join(t.TempDir(), "missing_signers"),
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

// SSH: no allowed_signers + require=false → loud warning, pass.
func TestTagSignature_SSH_NoAllowlistNoRequireWarns(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                "v1.0.0",
		AllowedSignersPath: filepath.Join(t.TempDir(), "missing_signers"),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if !strings.Contains(out.String(), "⚠️ No SSH signer allowlist") {
		t.Errorf("missing SSH no-allowlist warning:\n%s", out.String())
	}
}
