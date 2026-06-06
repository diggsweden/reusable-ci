// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
)

// fakeTagGit is shared by every TagSignature unit test. The struct
// captures the signals from VerifyTag(Signature) so each test can
// assert which verifier ran and what it was passed.

type fakeTagGit struct {
	body string
	// In-process verifier signals.
	verifySigSigner      string
	verifySigFingerprint string
	verifySigOK          bool
	verifySigErr         error
	verifySigCallCount   int
	verifySigArmor       []byte
	// SSH-allowlist verifier signals.
	sshAllowedOK     bool
	sshAllowedOutput string
	sshAllowedErr    error
}

func (f *fakeTagGit) RevParse(_ context.Context, ref string) (string, error) {
	return ref + "-sha", nil
}
func (f *fakeTagGit) TagsPointingAt(_ context.Context, _ string) ([]string, error) {
	return []string{"v1.0.0"}, nil //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}
func (f *fakeTagGit) IsAncestor(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (f *fakeTagGit) CatFileType(_ context.Context, _ string) (string, error) { return "tag", nil } //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
func (f *fakeTagGit) CatFileTag(_ context.Context, _ string) (string, error)  { return f.body, nil }
func (f *fakeTagGit) VerifyTagSignature(_ context.Context, _ string, armor []byte) (string, string, bool, error) {
	f.verifySigCallCount++

	f.verifySigArmor = append([]byte(nil), armor...)

	return f.verifySigSigner, f.verifySigFingerprint, f.verifySigOK, f.verifySigErr
}

func (f *fakeTagGit) VerifyTagSSHAgainstAllowedSigners(_ context.Context, _, _ string) (bool, string, error) {
	return f.sshAllowedOK, f.sshAllowedOutput, f.sshAllowedErr
}
func (f *fakeTagGit) TaggerInfo(_ context.Context, _ string) (string, string, error) {
	return "Alice <alice@example.com>", "2026-05-14", nil
}
func (f *fakeTagGit) TagMessage(_ context.Context, _ string) (string, error) {
	return "Release v1.0.0", nil
}
func (f *fakeTagGit) TagSHA(_ context.Context, _ string) (string, error) { return "abc1234", nil }

func TestTagSignature_GPGSignedVerifiesInProcessAndPrintsSigner(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{
		body:            "object abc\ntype commit\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifySigSigner: "Alice <alice@example.com>",
		verifySigOK:     true,
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, appvalidate.TagSignatureInput{
		Tag:                 "v1.0.0",
		ReleaseGPGPublicKey: []byte("public-key"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if gitr.verifySigCallCount != 1 {
		t.Errorf("VerifyTagSignature calls = %d, want 1", gitr.verifySigCallCount)
	}

	if !reflect.DeepEqual(gitr.verifySigArmor, []byte("public-key")) {
		t.Errorf("VerifyTagSignature armor = %q, want %q", gitr.verifySigArmor, "public-key")
	}

	for _, want := range []string{
		"GPG signature verified",
		"Signed by: Alice <alice@example.com>",
		"Tagged commit: abc1234",
		"Release v1.0.0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestTagSignature_GPGSignedWithoutPublicKeyIsInformational(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n"}

	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "verification requires signer's public key") {
		t.Errorf("missing unverified note:\n%s", out.String())
	}
}

func TestTagSignature_GPGSignedButVerifyFailsRendersFailureNote(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{
		body:        "-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifySigOK: false,
	}

	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, appvalidate.TagSignatureInput{
		Tag:                 "v1.0.0",
		ReleaseGPGPublicKey: []byte("wrong-key"),
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "did not verify against the configured public key") {
		t.Errorf("missing failure note:\n%s", out.String())
	}
}

func TestTagSignature_SSHSignedDoesNotRunGPGVerification(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}

	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}

	if gitr.verifySigCallCount != 0 {
		t.Errorf("VerifyTagSignature calls = %d, want 0 for SSH signatures", gitr.verifySigCallCount)
	}

	if !strings.Contains(out.String(), "SSH signature") {
		t.Errorf("missing SSH signature note:\n%s", out.String())
	}
}
