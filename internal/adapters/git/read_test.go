// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

func TestReleaseRequestGitHelpersUseHookDisabledRemoteCommands(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git", `
set -euo pipefail
case "$*" in
  '-c core.hooksPath=/dev/null fetch --no-tags origin refs/tags/release-request/v1.2.3:refs/tags/release-request/v1.2.3') ;;
  '-c core.hooksPath=/dev/null ls-remote --tags origin refs/tags/release-request/v1.2.3')
    printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/tags/release-request/v1.2.3\n'
    ;;
  '-c core.hooksPath=/dev/null ls-remote --tags origin refs/tags/v1.2.3') ;;
  *) echo "unexpected git args: $*" >&2; exit 1 ;;
esac
`)

	repo := &adaptergit.Repo{GitBin: m.Path("git")}
	ctx := context.Background()

	if err := repo.FetchTagFromRemote(ctx, "origin", "release-request/v1.2.3"); err != nil {
		t.Fatal(err)
	}

	obj, err := repo.RemoteTagObject(ctx, "origin", "release-request/v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if obj != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("remote object = %q", obj)
	}

	exists, err := repo.RemoteTagExists(ctx, "origin", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Error("final tag should not exist when ls-remote returns no rows")
	}

	invocations := m.Invocations("git")
	if len(invocations) != 3 {
		t.Fatalf("git invocations = %d, want 3", len(invocations))
	}

	for _, inv := range invocations {
		if !slices.Contains(inv.Args, "core.hooksPath=/dev/null") {
			t.Errorf("git args missing hook disable: %s", strings.Join(inv.Args, " "))
		}
	}
}

func TestVerifyTagSSHAgainstAllowedSigners_UsesSSHFormat(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("git", `
set -euo pipefail
if [ "$*" != '-c core.hooksPath=/dev/null -c gpg.format=ssh -c gpg.ssh.allowedSignersFile=allowed_signers tag -v release-request/v1.2.3' ]; then
  echo "unexpected git args: $*" >&2
  exit 1
fi
printf 'Good "git" signature for release-request/v1.2.3\n'
`)

	repo := &adaptergit.Repo{GitBin: m.Path("git")}

	ok, out, err := repo.VerifyTagSSHAgainstAllowedSigners(context.Background(), "release-request/v1.2.3", "allowed_signers")
	if err != nil {
		t.Fatal(err)
	}

	if !ok || !strings.Contains(out, "Good") {
		t.Fatalf("VerifyTagSSHAgainstAllowedSigners = (%v, %q), want ok output", ok, out)
	}
}

// TestVerifyTagSSHAgainstAllowedSigners_ClassifiesEachFailure covers the three
// outcomes an operator can hit and the sentinel each one carries.
//
// Only the success path was tested. The other three are the ones that decide
// what a failed release tells the operator to do: a missing allowed_signers
// file is missing input (exit 66, "commit this file"), a signature from a key
// that is not listed is a refusal (exit 77, "this signer is not authorised"),
// and anything else stays a plain validation failure. Collapsing any of them
// into another turns an actionable message into a generic one, and collapsing
// the refusal specifically would report an unauthorised signer with the same
// exit code as a broken repository.
func TestVerifyTagSSHAgainstAllowedSigners_ClassifiesEachFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stderr  string
		want    error
		wantMsg string
	}{
		{
			name:    "the allowed_signers file cannot be opened",
			stderr:  "error: Unable to open allowed keys file: allowed_signers\n",
			want:    errs.ErrMissingInput,
			wantMsg: "read SSH allowed_signers file",
		},
		{
			name:    "the file is absent",
			stderr:  "error: gpg.ssh.allowedSignersFile: No such file or directory\n",
			want:    errs.ErrMissingInput,
			wantMsg: "read SSH allowed_signers file",
		},
		{
			name:    "the signature is valid but the signer is not listed",
			stderr:  "Good \"git\" signature with ED25519 key SHA256:abc\nNo principal matched.\n",
			want:    errs.ErrPermissionDenied,
			wantMsg: "not in allowed_signers",
		},
		{
			name:   "an unrecognised failure stays a validation error",
			stderr: "error: could not read the tag object\n",
			want:   errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeGitBinary(t, "", tc.stderr, 1)
			repo := &adaptergit.Repo{GitBin: fake.path}

			ok, out, err := repo.VerifyTagSSHAgainstAllowedSigners(
				context.Background(), "v1.2.3", "allowed_signers")
			if ok {
				t.Error("a failing git must not report the tag as verified")
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want it to say %q", err, tc.wantMsg)
			}

			// The returned output is what a caller prints for context, so
			// git's own diagnostic has to survive into it.
			if !strings.Contains(out, strings.TrimSpace(strings.Split(tc.stderr, "\n")[0])) {
				t.Errorf("returned output dropped git's diagnostic: %q", out)
			}
		})
	}
}

// TestGitRun_PinsTheMessageLocale is the control for the test above.
//
// Every classification there matches an English string that git and ssh-keygen
// translate. Under a Swedish or German LANG the same failures come back with
// different words, every match misses, and the missing-file and unauthorised-
// signer cases both fall through to the generic branch. The tests would still
// pass, because they set the message themselves.
//
// So this asserts the property that makes them meaningful: the subprocess is
// run with the message locale pinned, whatever the host's locale is. The
// ambient locale is set to something non-English first, so "pinned" is
// distinguishable from "the machine running the tests is already in English".
func TestGitRun_PinsTheMessageLocale(t *testing.T) {
	t.Setenv("LANG", "sv_SE.UTF-8")
	t.Setenv("LC_MESSAGES", "sv_SE.UTF-8")
	t.Setenv("LC_ALL", "")

	fake := newFakeGitBinary(t, "ok", "", 0)
	repo := &adaptergit.Repo{GitBin: fake.path}

	if _, err := repo.Run(context.Background(), "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}

	if got := fake.env(t); !strings.Contains(got, "LC_ALL=C\n") {
		t.Errorf("git ran with:\n%s\nwant LC_ALL=C, or the message classification is a property of the host", got)
	}
}

// TestGitRun_PassesTheAmbientEnvironmentThrough is the other half of that fix.
//
// Pinning the locale means setting cmd.Env, and cmd.Env REPLACES the
// environment rather than adding to it. These same invocations reach ssh-agent,
// gpg-agent and credential helpers through variables like SSH_AUTH_SOCK and
// GNUPGHOME, so an env that carries only the locale would leave every signing
// and authenticated operation unable to find its agent — and it would fail at
// runtime on a real runner, not here.
func TestGitRun_PassesTheAmbientEnvironmentThrough(t *testing.T) {
	t.Setenv("RCI_TEST_PROBE", "owned-test-value")

	fake := newFakeGitBinary(t, "", "", 0)
	repo := &adaptergit.Repo{GitBin: fake.path}

	if _, err := repo.Run(context.Background(), "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}

	// What git saw, not what this process has: the point is that setting
	// cmd.Env did not discard the inherited environment.
	if got := fake.env(t); !strings.Contains(got, "RCI_TEST_PROBE=owned-test-value\n") {
		t.Errorf("git ran with:\n%s\nwant the inherited RCI_TEST_PROBE; cmd.Env replaced the environment instead of extending it", got)
	}
}

// TestGitRun_RedactsKeyMaterialFromADiagnostic proves the failure path does not
// forward key material into the error.
//
// Run appends git's combined output to the wrapped error so an operator can see
// what went wrong. git and the tools it drives read key files, and a failure
// that echoes one would put it in the error, which is printed and logged. The
// redaction is applied on that path already; nothing asserted it, so removing
// it would have been invisible.
func TestGitRun_RedactsKeyMaterialFromADiagnostic(t *testing.T) {
	// Base64-looking, but it is the literal string "secret-key-material":
	// deliberately recognisable so a leak is obvious in a failure message.
	const secret = "c2VjcmV0LWtleS1tYXRlcmlhbA" //nolint:gosec // G101: synthetic fixture, not a credential.

	fake := newFakeGitBinary(t, "",
		"error: bad key\n-----BEGIN OPENSSH PRIVATE KEY-----\n"+secret+"\n-----END OPENSSH PRIVATE KEY-----\n", 1)
	repo := &adaptergit.Repo{GitBin: fake.path}

	_, err := repo.Run(context.Background(), "tag", "-v", "v1.2.3")
	if err == nil {
		t.Fatal("a non-zero git exit must be an error")
	}

	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error carries key material:\n%s", err)
	}

	if !strings.Contains(err.Error(), "redacted") {
		t.Errorf("err = %v, want it to say the output was redacted", err)
	}
}

// TestGitRun_CancellationIsReported keeps a cancelled context distinguishable
// from a tool that simply failed. A release job cancelled mid-verification must
// not read as "this tag is not signed".
func TestGitRun_CancellationIsReported(t *testing.T) {
	fake := newFakeGitBinary(t, "", "", 0)
	repo := &adaptergit.Repo{GitBin: fake.path}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.Run(ctx, "rev-parse", "HEAD"); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to carry context.Canceled", err)
	}
}
