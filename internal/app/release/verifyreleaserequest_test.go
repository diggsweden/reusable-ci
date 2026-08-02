// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const requestObject = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var (
	errUnexpectedRef                 = errors.New("unexpected ref")
	errMissingLocalTag               = errors.New("missing local tag")
	errUnexpectedFetch               = errors.New("unexpected fetch")
	errUnexpectedRemoteRequestLookup = errors.New("unexpected remote request lookup")
	errUnexpectedFinalTagLookup      = errors.New("unexpected final tag lookup")
	errUnexpectedCatFileRef          = errors.New("unexpected cat-file ref")
)

type fakeReleaseRequestGit struct {
	localObject       string
	localMissingFirst bool
	remoteObject      string
	finalExists       bool
	catType           string
	verifyOK          bool
	verifyOutput      string
	verifyErr         error
	fetches           int
	verifiedTag       string
	verifiedSigners   string
}

func newFakeReleaseRequestGit() *fakeReleaseRequestGit {
	return &fakeReleaseRequestGit{
		localObject:  requestObject,
		remoteObject: requestObject,
		catType:      "tag",
		verifyOK:     true,
	}
}

func (f *fakeReleaseRequestGit) RevParse(_ context.Context, ref string) (string, error) {
	if ref != "refs/tags/release-request/v1.2.3" {
		return "", errUnexpectedRef
	}

	if f.localMissingFirst && f.fetches == 0 {
		return "", errMissingLocalTag
	}

	return f.localObject, nil
}

func (f *fakeReleaseRequestGit) FetchTagFromRemote(_ context.Context, remote, tag string) error {
	if remote != "origin" || tag != "release-request/v1.2.3" {
		return errUnexpectedFetch
	}

	f.fetches++

	return nil
}

func (f *fakeReleaseRequestGit) RemoteTagObject(_ context.Context, remote, tag string) (string, error) {
	if remote != "origin" || tag != "release-request/v1.2.3" {
		return "", errUnexpectedRemoteRequestLookup
	}

	if f.remoteObject == "" {
		return "", errs.ErrValidation
	}

	return f.remoteObject, nil
}

func (f *fakeReleaseRequestGit) RemoteTagExists(_ context.Context, remote, tag string) (bool, error) {
	if remote != "origin" || tag != "v1.2.3" {
		return false, errUnexpectedFinalTagLookup
	}

	return f.finalExists, nil
}

func (f *fakeReleaseRequestGit) CatFileType(_ context.Context, ref string) (string, error) {
	if ref != "refs/tags/release-request/v1.2.3" {
		return "", errUnexpectedCatFileRef
	}

	return f.catType, nil
}

func (f *fakeReleaseRequestGit) VerifyTagSSHAgainstAllowedSigners(_ context.Context, tag, allowedSignersPath string) (bool, string, error) {
	f.verifiedTag = tag
	f.verifiedSigners = allowedSignersPath

	return f.verifyOK, f.verifyOutput, f.verifyErr
}

func writeAllowedSigners(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "allowed_signers")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func runVerifyReleaseRequest(t *testing.T, git *fakeReleaseRequestGit, signers string, request, tag string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	err := apprelease.VerifyReleaseRequest(context.Background(), git, &out, apprelease.VerifyReleaseRequestInput{
		ReleaseRequest:     request,
		ReleaseTag:         tag,
		AllowedSignersPath: signers,
	})

	return out.String(), err
}

func TestVerifyReleaseRequest_Accepts(t *testing.T) {
	t.Parallel()

	git := newFakeReleaseRequestGit()
	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForUnitTestsOnly\n")

	out, err := runVerifyReleaseRequest(t, git, signers, "release-request/v1.2.3", "v1.2.3")
	if err != nil {
		t.Fatalf("valid release request rejected: %v", err)
	}

	if !strings.Contains(out, "Release request signature verified: release-request/v1.2.3 -> v1.2.3") {
		t.Errorf("success output = %q", out)
	}

	if git.verifiedTag != "release-request/v1.2.3" || git.verifiedSigners != signers {
		t.Errorf("SSH verify args = (%q, %q)", git.verifiedTag, git.verifiedSigners)
	}
}

func TestVerifyReleaseRequest_FetchesRequestTagWhenMissingLocally(t *testing.T) {
	t.Parallel()

	git := newFakeReleaseRequestGit()
	git.localMissingFirst = true
	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAA\n")

	if _, err := runVerifyReleaseRequest(t, git, signers, "release-request/v1.2.3", "v1.2.3"); err != nil {
		t.Fatal(err)
	}

	if git.fetches != 1 {
		t.Errorf("fetches = %d, want 1", git.fetches)
	}
}

func TestVerifyReleaseRequest_RejectsShellRegressionModes(t *testing.T) {
	t.Parallel()

	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAA\n")
	cases := map[string]func(*fakeReleaseRequestGit){
		"final_exists":  func(g *fakeReleaseRequestGit) { g.finalExists = true },
		"mismatch":      func(g *fakeReleaseRequestGit) { g.localObject = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
		"lightweight":   func(g *fakeReleaseRequestGit) { g.catType = "commit" },
		"bad_signature": func(g *fakeReleaseRequestGit) { g.verifyErr = errs.ErrValidation },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			git := newFakeReleaseRequestGit()
			mutate(git)

			if _, err := runVerifyReleaseRequest(t, git, signers, "release-request/v1.2.3", "v1.2.3"); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestVerifyReleaseRequest_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAA\n")
	cases := map[string]struct {
		request string
		tag     string
	}{
		"unstable_request": {"release-request/v1.2.3-rc1", "v1.2.3-rc1"},
		"unstable_tag":     {"release-request/v1.2.3", "v1.2.3-rc1"},
		"mismatched_tag":   {"release-request/v1.2.3", "v1.2.4"},
		"not_request":      {"v1.2.3", "v1.2.3"},
		"qualified_ref":    {"refs/tags/release-request/v1.2.3", "v1.2.3"},
		"multi_line":       {"release-request/v1.2.3\n", "v1.2.3"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runVerifyReleaseRequest(t, newFakeReleaseRequestGit(), signers, tc.request, tc.tag)
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}
		})
	}
}

func TestVerifyReleaseRequest_RejectsMissingOrEmptyAllowedSigners(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing_allowed_signers")
	empty := writeAllowedSigners(t, "")

	for name, path := range map[string]string{"missing": missing, "empty": empty} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runVerifyReleaseRequest(t, newFakeReleaseRequestGit(), path, "release-request/v1.2.3", "v1.2.3")
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}
		})
	}
}

func TestVerifyReleaseRequest_MissingInputsAreUsage(t *testing.T) {
	t.Parallel()

	err := apprelease.VerifyReleaseRequest(context.Background(), newFakeReleaseRequestGit(), nil, apprelease.VerifyReleaseRequestInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want usage", err)
	}
}

func TestVerifyReleaseRequest_RejectsRemoteRequestMissing(t *testing.T) {
	t.Parallel()

	git := newFakeReleaseRequestGit()
	git.remoteObject = ""
	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAA\n")

	_, err := runVerifyReleaseRequest(t, git, signers, "release-request/v1.2.3", "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestVerifyReleaseRequest_RejectsFalseVerificationWithoutError(t *testing.T) {
	t.Parallel()

	git := newFakeReleaseRequestGit()
	git.verifyOK = false
	git.verifyOutput = "No principal matched"
	signers := writeAllowedSigners(t, "release@example.test ssh-ed25519 AAAA\n")

	_, err := runVerifyReleaseRequest(t, git, signers, "release-request/v1.2.3", "v1.2.3")
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want permission denied", err)
	}
}
