// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// fakeTagGit is shared by every TagSignature unit test. The struct
// captures the signals from VerifyTag(Signature) so each test can
// assert which verifier ran and what it was passed.
//
// Every method also records the ref it was asked about. It used to discard
// them all, which meant no test could tell which tag was being validated:
// verifying a signature on some other tag than the requested one looked
// exactly like verifying the right one. See
// TestTagSignature_AsksOnlyAboutTheRequestedTag.

type fakeTagGit struct {
	body string
	// In-process verifier signals.
	verifySigSigner      string
	verifySigFingerprint string
	verifySigOK          bool
	verifySigErr         error
	verifySigCallCount   int
	verifySigArmor       []byte
	// sshVerify answers the SSH allowed_signers question. Nil fails loudly,
	// so a fixture that never meant to reach SSH verification cannot pass by
	// accident.
	sshVerify func(ref, signersPath string) (bool, string, error)
	// calls records every port call in order, so a test can assert both
	// which questions were asked and what they were asked about.
	calls []tagCall
}

// tagCall is one recorded port call. args holds the ref-shaped arguments in
// declaration order; the keyring and other blobs stay on their own fields.
type tagCall struct {
	method string
	args   []string
}

func (f *fakeTagGit) RevParse(_ context.Context, ref string) (string, error) {
	f.record("RevParse", ref)

	return ref + "-sha", nil
}

func (f *fakeTagGit) TagsPointingAt(_ context.Context, ref string) ([]string, error) {
	f.record("TagsPointingAt", ref)

	return []string{"v1.0.0"}, nil //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}

func (f *fakeTagGit) IsAncestor(_ context.Context, ancestor, descendant string) (bool, error) {
	f.record("IsAncestor", ancestor, descendant)

	return true, nil
}

func (f *fakeTagGit) CatFileType(_ context.Context, ref string) (string, error) {
	f.record("CatFileType", ref)

	return "tag", nil //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}

func (f *fakeTagGit) CatFileTag(_ context.Context, ref string) (string, error) {
	f.record("CatFileTag", ref)

	return f.body, nil
}

func (f *fakeTagGit) VerifyTagSignature(_ context.Context, ref string, armor []byte) (string, string, bool, error) {
	f.record("VerifyTagSignature", ref)
	f.verifySigCallCount++

	f.verifySigArmor = append([]byte(nil), armor...)

	return f.verifySigSigner, f.verifySigFingerprint, f.verifySigOK, f.verifySigErr
}

func (f *fakeTagGit) VerifyTagSSHAgainstAllowedSigners(_ context.Context, ref, signersPath string) (bool, string, error) {
	f.record("VerifyTagSSHAgainstAllowedSigners", ref, signersPath)

	if f.sshVerify == nil {
		return false, "", errUnexpectedSSHAllowlistCheck
	}

	return f.sshVerify(ref, signersPath)
}

// errUnexpectedSSHAllowlistCheck marks the stub being asked something the
// fixture did not configure it to answer.
var errUnexpectedSSHAllowlistCheck = errors.New("fakeTagGit: SSH allowed_signers verification was not configured by this fixture") //nolint:err113 // test fixture sentinel.

func (f *fakeTagGit) TaggerInfo(_ context.Context, ref string) (git.TaggerInfo, error) {
	f.record("TaggerInfo", ref)

	return git.TaggerInfo{Tagger: "Alice <alice@example.com>", Date: "2026-05-14"}, nil
}

func (f *fakeTagGit) TagMessage(_ context.Context, ref string) (string, error) {
	f.record("TagMessage", ref)

	return "Release v1.0.0", nil
}

func (f *fakeTagGit) TagSHA(_ context.Context, ref string) (string, error) {
	f.record("TagSHA", ref)

	return "abc1234", nil
}

func (f *fakeTagGit) TagExists(_ context.Context, ref string) (bool, error) {
	f.record("TagExists", ref)

	return false, nil
}

func (f *fakeTagGit) record(method string, args ...string) {
	f.calls = append(f.calls, tagCall{method: method, args: args})
}

// refsAskedAbout returns the first argument of every recorded call, which for
// this port is always the ref the caller is asking about.
func (f *fakeTagGit) refsAskedAbout() []string {
	refs := make([]string, 0, len(f.calls))

	for _, c := range f.calls {
		if len(c.args) > 0 {
			refs = append(refs, c.args[0])
		}
	}

	return refs
}

// methodsCalled returns the recorded call sequence.
func (f *fakeTagGit) methodsCalled() []string {
	names := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		names = append(names, c.method)
	}

	return names
}

func TestTagSignature_GPGSignedVerifiesInProcessAndPrintsSigner(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{
		body:            "object abc\ntype commit\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifySigSigner: "Alice <alice@example.com>",
		verifySigOK:     true,
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                 "v1.0.0",
		ReleaseGPGPublicKey: []byte("public-key"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if gitr.verifySigCallCount != 1 {
		t.Errorf("VerifyTagSignature calls = %d, want 1", gitr.verifySigCallCount)
	}

	if !bytes.Equal(gitr.verifySigArmor, []byte("public-key")) {
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
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "verification requires the signer's public key") {
		t.Errorf("missing unverified note:\n%s", out.String())
	}

	if !strings.Contains(out.String(), "::warning title=No release signer allowlist::") {
		t.Errorf("missing loud no-allowlist warning:\n%s", out.String())
	}
}

func TestTagSignature_GPGSignedButVerifyFailsRendersFailureNote(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{
		body:        "-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifySigOK: false,
	}

	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                 "v1.0.0",
		ReleaseGPGPublicKey: []byte("wrong-key"),
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "did not verify against any available key") {
		t.Errorf("missing failure note:\n%s", out.String())
	}
}

func TestTagSignature_SSHSignedDoesNotRunGPGVerification(t *testing.T) {
	t.Parallel()

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}

	var out bytes.Buffer
	if err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&out, output.FormatGitHub), appvalidate.TagSignatureInput{Tag: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}

	if gitr.verifySigCallCount != 0 {
		t.Errorf("VerifyTagSignature calls = %d, want 0 for SSH signatures", gitr.verifySigCallCount)
	}

	if !strings.Contains(out.String(), "SSH signature") {
		t.Errorf("missing SSH signature note:\n%s", out.String())
	}
}

// TestTagSignature_AsksOnlyAboutTheRequestedTag binds every Git question the
// validator asks to the tag it was told to validate.
//
// Nothing checked this before. fakeTagGit discarded every ref argument, so a
// validator that read the object type of the requested tag and then verified
// the signature on a DIFFERENT one produced identical, passing output in all
// fourteen GPG tests. That is the whole value of this command: it exists to say
// "this specific release tag is signed by someone we allow". Verifying the
// wrong tag answers a different question and still prints a tick.
//
// The fixture tag is deliberately not "v1.0.0". Every other fixture in this
// file uses that value, and the fake's own canned responses mention it, so a
// hardcoded ref inside the product would have matched by luck.
func TestTagSignature_AsksOnlyAboutTheRequestedTag(t *testing.T) {
	t.Parallel()

	const requested = "v4.11.2-audit"

	gitr := &fakeTagGit{
		body:                 "object abc\ntype commit\n-----BEGIN PGP SIGNATURE-----\n...\n-----END PGP SIGNATURE-----\n",
		verifySigOK:          true,
		verifySigSigner:      "Alice <alice@example.com>",
		verifySigFingerprint: "AAAABBBBCCCCDDDDEEEEFFFF0000111122223333",
	}

	var out bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out,
		output.NewAnnotator(&out, output.FormatGitHub),
		appvalidate.TagSignatureInput{Tag: requested, ReleaseGPGPublicKey: []byte("public-key")})
	if err != nil {
		t.Fatal(err)
	}

	if len(gitr.calls) == 0 {
		t.Fatal("no Git calls recorded; the assertions below would be vacuous")
	}

	for i, ref := range gitr.refsAskedAbout() {
		if ref != requested {
			t.Errorf("call %d (%s) asked about %q, want %q", i, gitr.calls[i].method, ref, requested)
		}
	}

	// The signature check specifically: it is the one call whose answer is a
	// security decision, so name it rather than relying on the loop above to
	// have covered it.
	if gitr.verifySigCallCount != 1 {
		t.Errorf("VerifyTagSignature called %d times, want exactly 1", gitr.verifySigCallCount)
	}

	want := []string{
		"CatFileType",
		"CatFileTag",
		"VerifyTagSignature",
		"TagSHA",
		"TaggerInfo",
		"TagMessage",
	}

	got := gitr.methodsCalled()
	if len(got) != len(want) {
		t.Fatalf("call sequence = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestTagSignature_SSHPathAlsoAsksAboutTheRequestedTag covers the other
// verifier. The SSH branch reaches its allowlist check with both the tag and
// the allowed-signers path, and both must be what the caller asked for: a
// signers file read from somewhere else is the same class of bug as verifying
// the wrong tag.
func TestTagSignature_SSHPathAlsoAsksAboutTheRequestedTag(t *testing.T) {
	t.Parallel()

	const requested = "v7.0.0-ssh-audit"

	signers := testfs.NewReal(t).WriteFile("allowed_signers",
		[]byte("alice@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyMaterialOnly\n"))

	gitr := &fakeTagGit{body: "-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"}

	var out bytes.Buffer

	// The stub refuses the allowlist question, so this call fails. The point
	// is what it was ASKED, which is recorded either way.
	_ = appvalidate.TagSignature(context.Background(), gitr, &out,
		output.NewAnnotator(&out, output.FormatGitHub),
		appvalidate.TagSignatureInput{Tag: requested, AllowedSignersPath: signers})

	var sshCall *tagCall

	for i := range gitr.calls {
		if gitr.calls[i].method == "VerifyTagSSHAgainstAllowedSigners" {
			sshCall = &gitr.calls[i]
		}
	}

	if sshCall == nil {
		t.Fatalf("the SSH allowlist verifier was never reached; calls = %v", gitr.methodsCalled())
	}

	if sshCall.args[0] != requested {
		t.Errorf("SSH verification asked about %q, want %q", sshCall.args[0], requested)
	}

	if sshCall.args[1] != signers {
		t.Errorf("SSH verification read %q, want the configured allowed-signers file %q", sshCall.args[1], signers)
	}
}
