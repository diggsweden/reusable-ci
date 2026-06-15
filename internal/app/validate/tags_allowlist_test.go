// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	adapteropenpgp "github.com/diggsweden/reusable-ci/internal/adapters/openpgp"
	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
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

func writeTemp(t *testing.T, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
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
		AllowedGPGKeysPath: writeTemp(t, "allowed_gpg_keys.asc", keyArmor),
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
		AllowedGPGKeysPath:       writeTemp(t, "allowed_gpg_keys.asc", keyArmor),
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
		AllowedGPGKeysPath:       writeTemp(t, "allowed_gpg_keys.asc", keyArmor),
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
