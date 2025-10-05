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
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	adapteropenpgp "github.com/diggsweden/reusable-ci/v3/internal/pgp"
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

// TestTagSignature_GPG_AllowlistHonoursEveryConcatenatedBlock covers the
// file shape the documentation tells operators to produce.
//
// docs/verification.md describes allowed_gpg_keys.asc as "one or more PGP
// PUBLIC KEY BLOCK sections concatenated" and says to add a key with
// `gpg --armor --export <email> >> .reusable-ci/allowed_gpg_keys.asc`.
// That append writes a second armor block. Reading only the first one
// denied a signer added exactly as documented -- silently, with the key
// count in the refusal as the only hint -- and it bit during key
// rotation, the one time the file deliberately holds two keys.
func TestTagSignature_GPG_AllowlistHonoursEveryConcatenatedBlock(t *testing.T) {
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
	if err != nil {
		t.Fatalf("the appended key was not authorised: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "Signer fingerprint is authorised") {
		t.Errorf("missing authorised note:\n%s", out.String())
	}
}

// TestTagSignature_GPG_V6KeyAuthorisesItsSigner covers the other length a
// primary-key fingerprint comes in.
//
// A v6 fingerprint (RFC 9580) is SHA-256, so 64 hex characters rather
// than v4's 40. The allowlist accepted only 40, so a committed v6 key
// derived an empty set and refused its own signer while naming
// "0 key(s)" for a file the operator can see holds a key. gnupg
// generates v6 from 2.5.x, so the length is the only thing that has to
// change for a project to hit it.
func TestTagSignature_GPG_V6KeyAuthorisesItsSigner(t *testing.T) {
	t.Parallel()

	keyArmor, fpr := armoredV6PublicKey(t)

	if len(fpr) != 64 {
		t.Fatalf("fixture is not a v6 fingerprint (%d chars) -- this test no longer covers the v6 length", len(fpr))
	}

	gitr := &fakeTagGit{
		body:                 gpgSignedTagBody,
		verifySigOK:          true,
		verifySigFingerprint: fpr,
		verifySigSigner:      "V6 Signer <v6@example.com>",
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: true,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, keyArmor),
	})
	if err != nil {
		t.Fatalf("a committed v6 key did not authorise its own signer: %v\n%s", err, out.String())
	}
}

// TestTagSignature_GPG_UnparsableAllowlistIsNotNoAllowlist is the
// fail-open direction, and the one that would matter.
//
// A present file that yields no usable fingerprints must never be
// treated as "no allowlist": that path warns and accepts any valid
// signature when require-authorization is off. allowlistPresent keys off
// the file having bytes rather than off the derived set, so the refusal
// has to come from the parse instead -- which is what this pins.
func TestTagSignature_GPG_UnparsableAllowlistIsNotNoAllowlist(t *testing.T) {
	t.Parallel()

	_, fpr := armoredPublicKey(t)

	gitr := &fakeTagGit{
		body:                 gpgSignedTagBody,
		verifySigOK:          true,
		verifySigFingerprint: fpr,
		verifySigSigner:      "Allowed Signer <signer@example.com>",
	}

	// Present, non-empty, and not a key: the shape a truncated or
	// hand-edited bundle takes.
	notAKey := []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nnot base64 at all\n-----END PGP PUBLIC KEY BLOCK-----\n")

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: false,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, notAKey),
	})
	// Malformed rather than merely non-nil: a refusal because the
	// derived set came out empty would satisfy err != nil while the file
	// was still being read in part, which is the defect next door.
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput\n%s", err, out.String())
	}

	if strings.Contains(out.String(), "NO signer allowlist") {
		t.Errorf("reported as having no allowlist, but the file is present:\n%s", out.String())
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

// armoredV6PublicKey mints a v6 (RFC 9580) entity and returns its
// armored public key plus the fingerprint PrimaryFingerprints derives
// from it -- 64 hex characters, because a v6 primary-key fingerprint is
// 32 bytes rather than v4's 20.
func armoredV6PublicKey(t *testing.T) ([]byte, string) {
	t.Helper()

	entity, err := gocrypto.NewEntity("V6 Signer", "ci", "v6@example.com", &packet.Config{V6Keys: true})
	if err != nil {
		t.Skipf("cannot mint a v6 key with this go-crypto: %v", err)
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

	if len(fps[0]) != 64 {
		t.Fatalf("fixture is not a v6 key: fingerprint is %d characters", len(fps[0]))
	}

	return buf.Bytes(), fps[0]
}

// The warn-and-proceed branch had no test at all, though its
// fail-closed twin did (TestTagSignature_GPG_UnverifiableRequireFailsClosed,
// same inputs with the flag on). It is the one place where a release
// continues despite a tag signature that could not be checked against
// the allowlist: an allowlist IS present, the signature did not verify
// against it, and require-authorization is off.
//
// Two things have to hold, and only one of them is obvious. The obvious
// one is that it warns. The other is that it warns *loudly enough to be
// seen*: the annotation is what lands in the forge's annotations pane,
// and a release that silently proceeded on an unverifiable signature
// with only a line of stdout would look identical to one that verified.

// TestTagSignature_GPG_UnverifiableWarnsAndProceeds covers the branch
// itself. The signer's key is not in the committed bundle, so
// verification cannot conclude; with enforcement off, that is a warning.
func TestTagSignature_GPG_UnverifiableWarnsAndProceeds(t *testing.T) {
	t.Parallel()

	keyArmor, _ := armoredPublicKey(t)

	// verifySigOK=false with an allowlist present: the tag is signed by
	// somebody, but not by anyone whose key is committed.
	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: false}

	var out bytes.Buffer

	annot := output.NewAnnotator(&out, output.FormatGitHub)

	err := appvalidate.TagSignature(context.Background(), gitr, &out, annot, appvalidate.TagSignatureInput{
		Tag:                      "v1.0.0",
		RequireAllowlistedSigner: false,
		AllowedGPGKeysPath:       writeGPGKeysFile(t, keyArmor),
	})
	if err != nil {
		t.Fatalf("with require-authorization off this must warn, not fail: %v\n%s", err, out.String())
	}

	body := out.String()

	// The forge-visible annotation, not just the stdout line. On GitHub
	// this is the ::warning:: form that reaches the annotations pane.
	if !strings.Contains(body, "::warning") {
		t.Errorf("no forge annotation was emitted, so the skip is invisible in the run summary:\n%s", body)
	}

	if !strings.Contains(body, "enforcement skipped") {
		t.Errorf("the operator is not told enforcement was skipped:\n%s", body)
	}

	// It must not read as a success.
	if strings.Contains(body, "Signer fingerprint is authorised") {
		t.Errorf("an unverifiable signature was reported as authorised:\n%s", body)
	}
}

// TestTagSignature_GPG_UnverifiableIsNotTheNoAllowlistWarning keeps the
// two warn-and-proceed paths distinct. "No allowlist committed" and
// "an allowlist exists but this signature does not match it" call for
// different actions from the operator, and the second is the more
// serious: somebody signed the tag with a key nobody authorised.
func TestTagSignature_GPG_UnverifiableIsNotTheNoAllowlistWarning(t *testing.T) {
	t.Parallel()

	keyArmor, _ := armoredPublicKey(t)
	gitr := &fakeTagGit{body: gpgSignedTagBody, verifySigOK: false}

	var out bytes.Buffer

	if err := appvalidate.TagSignature(context.Background(), gitr, &out,
		output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
			Tag:                      "v1.0.0",
			RequireAllowlistedSigner: false,
			AllowedGPGKeysPath:       writeGPGKeysFile(t, keyArmor),
		}); err != nil {
		t.Fatal(err)
	}

	body := out.String()
	if strings.Contains(body, "NO signer allowlist") {
		t.Errorf("reported as having no allowlist, but one is committed:\n%s", body)
	}

	if !strings.Contains(body, "could not be verified") {
		t.Errorf("the warning does not say the signature was unverifiable:\n%s", body)
	}
}
