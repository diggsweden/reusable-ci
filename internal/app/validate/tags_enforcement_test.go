// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const sshSignedTagBody = "object abc\ntype commit\n-----BEGIN SSH SIGNATURE-----\n...\n-----END SSH SIGNATURE-----\n"

// enforcement is one combination of the two independent enforcement flags.
type enforcement struct {
	allowlisted, valid bool
}

func (e enforcement) String() string {
	return fmt.Sprintf("require-allowlisted=%t,require-valid=%t", e.allowlisted, e.valid)
}

func (e enforcement) any() bool { return e.allowlisted || e.valid }

func enforcementModes() []enforcement {
	return []enforcement{{false, false}, {true, false}, {false, true}, {true, true}}
}

// gpgCase is one GPG allowlist state and verifier outcome under one mode.
type gpgCase struct {
	allowlist string // absent, present or malformed
	outcome   string
	verifier  fakeTagGit
	mode      enforcement
}

// gpgExpectation is what the policy gives a case: its class, its warning and
// the number of verifier calls.
type gpgExpectation struct {
	err     error
	warning string
	calls   int
}

func (c gpgCase) expect() gpgExpectation {
	verified := c.verifier.verifySigOK && c.verifier.verifySigErr == nil

	switch c.allowlist {
	case "malformed":
		return gpgExpectation{err: errs.ErrMalformedInput}
	case "absent":
		switch {
		case c.mode.allowlisted, c.mode.valid && !verified:
			return gpgExpectation{err: errs.ErrPermissionDenied, calls: 1}
		case c.mode.valid:
			return gpgExpectation{calls: 1}
		default:
			return gpgExpectation{warning: "No GPG signer allowlist present", calls: 1}
		}
	}

	switch {
	case verified && c.outcome == "signer not in allowlist", !verified && c.mode.any():
		return gpgExpectation{err: errs.ErrPermissionDenied, calls: 1}
	case !verified:
		return gpgExpectation{warning: "GPG signature could not be verified against the allowlist", calls: 1}
	default:
		return gpgExpectation{calls: 1}
	}
}

// TestTagSignature_GPGEnforcementMatrix runs every GPG allowlist state against
// every verifier outcome under every enforcement combination:
//
//   - a present allowlist that is not a key bundle is malformed input before
//     anything is verified, whatever the flags say; it used to be verified
//     against, fail, and pass with a warning when enforcement was off;
//   - with no allowlist, require-allowlisted refuses, require-valid refuses
//     only an unverified signature, and neither warns and passes;
//   - with an allowlist, a signer in it passes and one outside it is refused
//     regardless of the flags, while an unverified signature is refused under
//     either flag and warns and passes under neither.
//
// The verifier is asked once about the requested tag, with the committed keys
// as its keyring.
func TestTagSignature_GPGEnforcementMatrix(t *testing.T) {
	t.Parallel()

	keyArmor, allowedFingerprint := armoredPublicKey(t)

	outcomes := map[string]fakeTagGit{
		"signer in allowlist":     {verifySigOK: true, verifySigFingerprint: allowedFingerprint},
		"signer not in allowlist": {verifySigOK: true, verifySigFingerprint: strings.Repeat("0", 40)},
		"not verified":            {},
		"verifier error":          {verifySigErr: fmt.Errorf("verify tag: %w", errs.ErrMalformedInput)},
	}

	allowlists := []string{"absent", "present", "malformed"}
	cases := make([]gpgCase, 0, len(allowlists)*len(outcomes)*len(enforcementModes()))

	for _, allowlist := range allowlists {
		for outcome, verifier := range outcomes {
			for _, mode := range enforcementModes() {
				cases = append(cases, gpgCase{allowlist: allowlist, outcome: outcome, verifier: verifier, mode: mode})
			}
		}
	}

	for _, c := range cases {
		t.Run(c.allowlist+"/"+c.outcome+"/"+c.mode.String(), func(t *testing.T) {
			t.Parallel()
			checkGPGEnforcement(t, c, keyArmor)
		})
	}
}

func checkGPGEnforcement(t *testing.T, c gpgCase, keyArmor []byte) {
	t.Helper()

	const tag = "v4.2.0-matrix"

	path := filepath.Join(t.TempDir(), "missing.asc")
	keyring := []byte(nil)

	switch c.allowlist {
	case "present":
		path, keyring = writeGPGKeysFile(t, keyArmor), bytes.TrimSpace(keyArmor)
	case "malformed":
		path = writeGPGKeysFile(t, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nnot base64 at all\n-----END PGP PUBLIC KEY BLOCK-----\n"))
	}

	gitr := c.verifier
	gitr.body = gpgSignedTagBody

	var out, annot bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), &gitr, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      tag,
		RequireAllowlistedSigner: c.mode.allowlisted,
		RequireValidSignature:    c.mode.valid,
		AllowedGPGKeysPath:       path,
	})

	want := c.expect()
	assertTagSignatureOutcome(t, err, want.err, out.String())
	assertTagWarning(t, want.warning, out.String(), annot.String())

	if gitr.verifySigCallCount != want.calls {
		t.Fatalf("VerifyTagSignature calls = %d, want %d; calls = %v", gitr.verifySigCallCount, want.calls, gitr.methodsCalled())
	}

	if want.calls == 1 && !bytes.Equal(gitr.verifySigArmor, keyring) {
		t.Errorf("keyring = %q, want %q", gitr.verifySigArmor, keyring)
	}

	for _, ref := range gitr.refsAskedAbout() {
		if ref != tag {
			t.Errorf("asked about %q, want only %q", ref, tag)
		}
	}
}

// sshAnswer is what the SSH allowed_signers verifier says, and the class the
// policy gives it when the file is present.
type sshAnswer struct {
	ok     bool
	output string
	err    error
	want   error
}

// TestTagSignature_SSHEnforcementMatrix runs a missing and a present
// allowed_signers file against every verifier answer under every enforcement
// combination. A missing file is refused under either flag and warns and
// passes under neither, without asking the verifier. A present file is always
// authoritative: the verifier is asked once with the requested tag and the
// configured path; acceptance passes and prints git's line, "false" with no
// error and a signer refusal are permission denied, and an infrastructure
// failure keeps its own class instead of reading as a refused signer.
func TestTagSignature_SSHEnforcementMatrix(t *testing.T) {
	t.Parallel()

	answers := map[string]sshAnswer{
		"accepted":       {ok: true, output: `Good "git" signature for alice@example.com with ED25519 key SHA256:x`},
		"false and nil":  {output: "unexplained", want: errs.ErrPermissionDenied},
		"signer refused": {err: fmt.Errorf("signer is not in allowed_signers: %w", errs.ErrPermissionDenied), want: errs.ErrPermissionDenied},
		"infrastructure": {err: fmt.Errorf("git not found in $PATH: %w", errs.ErrDependencyUnavailable), want: errs.ErrDependencyUnavailable},
	}

	for _, present := range []bool{false, true} {
		for name, answer := range answers {
			for _, mode := range enforcementModes() {
				t.Run(fmt.Sprintf("present=%t/%s/%s", present, name, mode), func(t *testing.T) {
					t.Parallel()
					checkSSHEnforcement(t, present, answer, mode)
				})
			}
		}
	}
}

func checkSSHEnforcement(t *testing.T, present bool, answer sshAnswer, mode enforcement) {
	t.Helper()

	const tag = "v4.2.0-ssh-matrix"

	signers := filepath.Join(t.TempDir(), "allowed_signers")
	if present {
		signers = testfs.NewReal(t).WriteFile("allowed_signers", []byte("alice@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyMaterialOnly\n"))
	}

	var asked [][2]string

	gitr := &fakeTagGit{
		body: sshSignedTagBody,
		sshVerify: func(ref, path string) (bool, string, error) {
			asked = append(asked, [2]string{ref, path})

			return answer.ok, answer.output, answer.err
		},
	}

	var out, annot bytes.Buffer

	err := appvalidate.TagSignature(context.Background(), gitr, &out, output.NewAnnotator(&annot, output.FormatGitHub), appvalidate.TagSignatureInput{
		Tag:                      tag,
		RequireAllowlistedSigner: mode.allowlisted,
		RequireValidSignature:    mode.valid,
		AllowedSignersPath:       signers,
	})

	if !present {
		want, warning := error(nil), "No SSH signer allowlist present"
		if mode.any() {
			want, warning = errs.ErrPermissionDenied, ""
		}

		assertTagSignatureOutcome(t, err, want, out.String())
		assertTagWarning(t, warning, out.String(), annot.String())

		if len(asked) != 0 {
			t.Errorf("verifier asked %v without an allowed_signers file", asked)
		}

		return
	}

	assertTagSignatureOutcome(t, err, answer.want, out.String())
	assertTagWarning(t, "", out.String(), annot.String())

	if len(asked) != 1 || asked[0] != [2]string{tag, signers} {
		t.Fatalf("verifier asked %v, want once about (%q, %q)", asked, tag, signers)
	}

	if errors.Is(answer.want, errs.ErrDependencyUnavailable) && errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("an infrastructure failure read as a refused signer: %v", err)
	}

	if answer.want == nil && !strings.Contains(out.String(), "SSH signature verified against "+signers+"\n   "+answer.output+"\n") {
		t.Errorf("accepted signature not reported with git's line:\n%s", out.String())
	}
}

// assertTagSignatureOutcome checks the class of err, and that only a passing
// run claims the release security requirements are met.
func assertTagSignatureOutcome(t *testing.T, err, want error, out string) {
	t.Helper()

	if want == nil && err != nil {
		t.Fatalf("err = %v, want nil\n%s", err, out)
	}

	if want != nil && !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v\n%s", err, want, out)
	}

	if passed := strings.Contains(out, "Release security requirements met"); passed != (want == nil) {
		t.Errorf("success summary printed = %t, want %t:\n%s", passed, want == nil, out)
	}
}

// assertTagWarning checks that the named warning, and only it, reached both
// the log and the annotations; an empty want means no warning at all.
func assertTagWarning(t *testing.T, want, out, annotations string) {
	t.Helper()

	if want == "" {
		if strings.Contains(out, "⚠️") || strings.Contains(annotations, "::warning") {
			t.Errorf("unexpected warning:\n%s%s", out, annotations)
		}

		return
	}

	if !strings.Contains(out, "⚠️ "+want) || !strings.Contains(annotations, "::warning ") {
		t.Errorf("missing warning %q:\nout:\n%s\nannotations:\n%s", want, out, annotations)
	}
}
