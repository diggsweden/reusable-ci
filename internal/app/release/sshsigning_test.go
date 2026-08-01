// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// fakeGitOps records the git config writes SSHSigningSetup makes.
type fakeGitOps struct {
	cfg map[string]string
}

func newFakeGitOps() *fakeGitOps { return &fakeGitOps{cfg: map[string]string{}} }

func (f *fakeGitOps) Config(_ context.Context, key, value string) error {
	f.cfg[key] = value

	return nil
}

func (f *fakeGitOps) Run(_ context.Context, args ...string) (string, error) {
	// Mirror the --global config path: ["config","--global",key,value].
	if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
		f.cfg[args[2]] = args[3]
	}

	return "", nil
}

func TestSSHSigningSetup_ConfiguresGit(t *testing.T) {
	t.Parallel()

	git := newFakeGitOps()

	var wrote []byte

	writeKey := func(key []byte) (string, error) {
		wrote = key

		return "/tmp/reusable-ci/ssh-signing-key", nil
	}

	err := apprelease.SSHSigningSetup(context.Background(), git, writeKey, apprelease.SSHSigningInput{
		PrivateKey:    "-----BEGIN OPENSSH PRIVATE KEY-----\nabc", // no trailing newline on purpose
		AuthorName:    "Release Bot",
		AuthorEmail:   "bot@example.org",
		GitCommitSign: true,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(string(wrote), "\n") {
		t.Errorf("written key must be newline-terminated, got %q", wrote)
	}

	want := map[string]string{
		"gpg.format":      "ssh",
		"user.signingkey": "/tmp/reusable-ci/ssh-signing-key",
		"user.name":       "Release Bot",
		"user.email":      "bot@example.org",
		"commit.gpgsign":  "true",
	}
	for k, v := range want {
		if git.cfg[k] != v {
			t.Errorf("git config %s = %q, want %q", k, git.cfg[k], v)
		}
	}
}

func TestSSHSigningSetup_NoCommitSignOmitsGpgsign(t *testing.T) {
	t.Parallel()

	git := newFakeGitOps()
	writeKey := func([]byte) (string, error) { return "/k", nil }

	err := apprelease.SSHSigningSetup(context.Background(), git, writeKey, apprelease.SSHSigningInput{
		PrivateKey: "key",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if _, set := git.cfg["commit.gpgsign"]; set {
		t.Error("commit.gpgsign must not be written when GitCommitSign is false")
	}

	if git.cfg["gpg.format"] != "ssh" {
		t.Errorf("gpg.format = %q, want ssh", git.cfg["gpg.format"])
	}
}

func TestSSHSigningSetup_RequiresKey(t *testing.T) {
	t.Parallel()

	git := newFakeGitOps()
	writeKey := func([]byte) (string, error) { return "/k", nil }

	err := apprelease.SSHSigningSetup(context.Background(), git, writeKey, apprelease.SSHSigningInput{
		PrivateKey: "   ",
	}, io.Discard)
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("blank PrivateKey should be ErrUsage, got %v", err)
	}
}
