// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
)

type fakeTagGit struct {
	body        string
	verifyOut   string
	verifyOK    bool
	verifyCalls int
}

func (f *fakeTagGit) RevParse(_ context.Context, ref string) (string, error) {
	return ref + "-sha", nil
}
func (f *fakeTagGit) TagsPointingAt(_ context.Context, _ string) ([]string, error) {
	return []string{"v1.0.0"}, nil
}
func (f *fakeTagGit) IsAncestor(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (f *fakeTagGit) CatFileType(_ context.Context, _ string) (string, error) { return "tag", nil }
func (f *fakeTagGit) CatFileTag(_ context.Context, _ string) (string, error)  { return f.body, nil }
func (f *fakeTagGit) VerifyTag(_ context.Context, _ string) (string, bool, error) {
	f.verifyCalls++
	return f.verifyOut, f.verifyOK, nil
}
func (f *fakeTagGit) TaggerInfo(_ context.Context, _ string) (string, string, error) {
	return "Alice <alice@example.com>", "2026-05-14", nil
}
func (f *fakeTagGit) TagMessage(_ context.Context, _ string) (string, error) {
	return "Release v1.0.0", nil
}
func (f *fakeTagGit) TagSHA(_ context.Context, _ string) (string, error) { return "abc1234", nil }

type fakeGPG struct{ imported [][]byte }

func (f *fakeGPG) ImportKey(_ context.Context, keyData []byte) error {
	f.imported = append(f.imported, append([]byte(nil), keyData...))
	return nil
}

func TestTagSignature_GPGSignedImportsKeyAndPrintsSigner(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{
		body:      "object abc\ntype commit\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifyOut: `gpg: Good signature from "Alice <alice@example.com>"`,
		verifyOK:  true,
	}
	gpg := &fakeGPG{}
	var out bytes.Buffer
	err := appvalidate.TagSignature(context.Background(), gitr, gpg, &out, appvalidate.TagSignatureInput{
		Tag:                 "v1.0.0",
		ReleaseGPGPublicKey: []byte("public-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gpg.imported, [][]byte{[]byte("public-key")}) {
		t.Errorf("imported keys = %q", gpg.imported)
	}
	for _, want := range []string{
		"GPG signature verification successful",
		"Signed by: Alice <alice@example.com>",
		"Tagged commit: abc1234",
		"Release v1.0.0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestTagSignature_GPGSignedButUnverifiedIsInformational(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n"}
	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, nil, &out, appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "verification requires signer's public key") {
		t.Errorf("missing unverified note:\n%s", out.String())
	}
}

func TestTagSignature_SSHSignedDoesNotRunGPGVerification(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}
	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, nil, &out, appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if gitr.verifyCalls != 0 {
		t.Errorf("VerifyTag calls = %d, want 0 for SSH signatures", gitr.verifyCalls)
	}
	if !strings.Contains(out.String(), "SSH signature present") {
		t.Errorf("missing SSH note:\n%s", out.String())
	}
}
