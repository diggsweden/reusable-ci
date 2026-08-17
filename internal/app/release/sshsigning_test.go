// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// fakeGitOps records the git config writes SSHSigningSetup makes, keeping the
// repo-local ones apart from the --global ones.
//
// It used to fold both into a single map. That is what let the --global path
// go unwatched: the GitConfigGlobal flag chooses which of these two methods is
// called, and a fake that cannot tell them apart cannot tell configuring a
// checkout from configuring the whole runner's ~/.gitconfig.
type fakeGitOps struct {
	local  map[string]string
	global map[string]string
	other  [][]string // any git invocation that was not a config write
}

func newFakeGitOps() *fakeGitOps {
	return &fakeGitOps{local: map[string]string{}, global: map[string]string{}}
}

func (f *fakeGitOps) Config(_ context.Context, key, value string) error {
	f.local[key] = value

	return nil
}

func (f *fakeGitOps) Run(_ context.Context, args ...string) (string, error) {
	// The --global config path: ["config","--global",key,value].
	if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
		f.global[args[2]] = args[3]

		return "", nil
	}

	f.other = append(f.other, args)

	return "", nil
}

// The key path the fake writer reports back. It is never opened — the writer
// is the seam — so it only has to look like the path the real one returns.
const fakeSSHKeyPath = "ssh-signing-key"

// TestSSHSigningSetup_InstallsTheKeyAndPointsGitAtIt covers the whole setup:
// the key is handed to the writer newline-terminated, because OpenSSH refuses
// to read a private key whose last line is not, and git is then pointed at
// exactly the settings that switch signing to SSH.
//
// The config is compared as a whole rather than key by key. These are writes
// into git's configuration, so a stray extra key is the interesting failure
// and checking only the wanted keys could not see one.
func TestSSHSigningSetup_InstallsTheKeyAndPointsGitAtIt(t *testing.T) {
	t.Parallel()

	git := newFakeGitOps()

	var wrote []byte

	writeKey := func(key []byte) (string, error) {
		wrote = key

		return fakeSSHKeyPath, nil
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
		"user.signingkey": fakeSSHKeyPath,
		"user.name":       "Release Bot",
		"user.email":      "bot@example.org",
		"commit.gpgsign":  "true",
	}
	if !reflect.DeepEqual(git.local, want) {
		t.Errorf("git config = %v\nwant %v", git.local, want)
	}
}

// TestSSHSigningSetup_TouchesGlobalGitConfigOnlyWhenAsked pins which of the two
// write paths is taken. Without the flag the settings belong to the checkout;
// with it they are written to the runner's own ~/.gitconfig, which outlives the
// job. Nothing exercised the --global branch before, so the flag could have
// been ignored in either direction unnoticed.
func TestSSHSigningSetup_TouchesGlobalGitConfigOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	for name, global := range map[string]bool{
		"repo-local by default": false,
		"global when asked":     true,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			git := newFakeGitOps()
			writeKey := func([]byte) (string, error) { return fakeSSHKeyPath, nil }

			err := apprelease.SSHSigningSetup(context.Background(), git, writeKey, apprelease.SSHSigningInput{
				PrivateKey:      "key",
				GitConfigGlobal: global,
			}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}

			wrote, untouched := git.local, git.global
			if global {
				wrote, untouched = git.global, git.local
			}

			if wrote["gpg.format"] != "ssh" || wrote["user.signingkey"] != fakeSSHKeyPath {
				t.Errorf("expected config not written: %v", wrote)
			}

			if len(untouched) != 0 {
				t.Errorf("wrote to the wrong config scope: %v", untouched)
			}

			if len(git.other) != 0 {
				t.Errorf("unexpected git invocations: %v", git.other)
			}
		})
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

	if _, set := git.local["commit.gpgsign"]; set {
		t.Error("commit.gpgsign must not be written when GitCommitSign is false")
	}

	if git.local["gpg.format"] != "ssh" {
		t.Errorf("gpg.format = %q, want ssh", git.local["gpg.format"])
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
