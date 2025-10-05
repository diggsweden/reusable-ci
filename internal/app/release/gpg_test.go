//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/gpgkey"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// gpgRoundTrip wipes the throwaway key, then re-imports it via app.GPGImport.
func gpgRoundTrip(t *testing.T, key *gpgkey.Key, sink *fakeoutputsink.Sink, in apprelease.GPGImportInput, gitRepo *git.Repo) string {
	t.Helper()
	if in.PrivateKey == "" {
		in.PrivateKey = key.ArmoredPrivateKey()
	}

	a := adaptergpg.New()
	ctx := context.Background()
	a.DeleteSecretKey(ctx, key.Fingerprint)
	a.DeleteKey(ctx, key.Fingerprint)

	if got := secretKeyFingerprints(t, key.GNUPGHOME); len(got) != 0 {
		t.Fatalf("keyring still holds secret keys %v before the import", got)
	}

	var out bytes.Buffer
	md, err := apprelease.GPGImport(ctx, a, openpgp.ReadMetadata, gitRepo, sink, in, &out)
	if err != nil {
		t.Fatalf("GPGImport: %v", err)
	}

	// The metadata above is read from the input itself, so only the keyring
	// shows the import happened: exactly this secret key, and nothing else.
	if got := secretKeyFingerprints(t, key.GNUPGHOME); !slices.Equal(got, []string{key.Fingerprint}) {
		t.Fatalf("secret keys after import = %v, want exactly %s", got, key.Fingerprint)
	}
	if md.Fingerprint != key.Fingerprint {
		t.Errorf("Fingerprint = %q, want %q", md.Fingerprint, key.Fingerprint)
	}
	if md.KeyID != key.KeyID() {
		t.Errorf("KeyID = %q, want %q", md.KeyID, key.KeyID())
	}
	if md.Name != key.Name {
		t.Errorf("Name = %q, want %q", md.Name, key.Name)
	}
	if md.Email != key.Email {
		t.Errorf("Email = %q, want %q", md.Email, key.Email)
	}
	return out.String()
}

// secretKeyFingerprints lists the primary fingerprints of the secret keys in
// home, read with gpg directly rather than through the adapter under test.
func secretKeyFingerprints(t *testing.T, home string) []string {
	t.Helper()

	//nolint:gosec // test infra; home is the key's own temp GNUPGHOME.
	out, err := exec.CommandContext(t.Context(), "gpg", "--homedir", home, "--batch", "--with-colons", "--list-secret-keys").Output()
	if err != nil && !strings.Contains(string(out), "sec:") {
		// gpg exits non-zero for an empty keyring on some versions; an empty
		// listing is the answer then.
		return nil
	}

	var fingerprints []string

	primary := false

	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Split(line, ":")

		switch {
		case fields[0] == "sec":
			primary = true
		case fields[0] == "fpr" && primary && len(fields) > 9:
			fingerprints = append(fingerprints, fields[9])
			primary = false
		case fields[0] == "ssb":
			primary = false
		}
	}

	return fingerprints
}

func TestGPGImport_EmitsExpectedOutputs(t *testing.T) {
	k := gpgkey.New(t)
	sink := fakeoutputsink.New(t)
	log := gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{}, nil)

	for key, want := range map[string]string{
		"fingerprint": k.Fingerprint,
		"keyid":       k.KeyID(),
		"name":        k.Name,
		"email":       k.Email,
	} {
		if got := sink.Single(key); got != want {
			t.Errorf("output %q = %q, want %q", key, got, want)
		}
	}

	for _, want := range []string{
		"Fingerprint : " + k.Fingerprint,
		"KeyID       : " + k.KeyID(),
		"Name        : " + k.Name,
		"Email       : " + k.Email,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
}

func TestGPGImport_RejectsEmptyKey(t *testing.T) {
	// A real gpg adapter is constructed here, so scrub the environment even
	// though the input is refused before any gpg command runs. Without this
	// the test's safety depends on that ordering holding.
	isolatedenv.Isolate(t)

	sink := fakeoutputsink.New(t)
	_, err := apprelease.GPGImport(context.Background(), adaptergpg.New(), openpgp.ReadMetadata, nil, sink,
		apprelease.GPGImportInput{}, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "PrivateKey is required") {
		t.Errorf("err = %v, want ErrUsage naming the missing key", err)
	}

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q for a refused import", got)
	}
}

func TestGPGImport_AcceptsBase64EncodedArmoredKey(t *testing.T) {
	k := gpgkey.New(t)
	sink := fakeoutputsink.New(t)
	gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{
		PrivateKey: base64.StdEncoding.EncodeToString([]byte(k.ArmoredPrivateKey())),
	}, nil)

	if got := sink.Single("fingerprint"); got != k.Fingerprint {
		t.Errorf("fingerprint = %q, want %q", got, k.Fingerprint)
	}
}

func TestGPGImport_ConfiguresGitSigning(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	gitRepo := &git.Repo{Dir: repo.Dir}

	k := gpgkey.New(t)
	sink := fakeoutputsink.New(t)
	gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{
		GitUserSigningKey: true,
		GitCommitGPGSign:  true,
	}, gitRepo)

	if got := repo.Git("config", "--get", "user.signingkey"); got != k.KeyID() {
		t.Errorf("user.signingkey = %q, want %q", got, k.KeyID())
	}
	if got := repo.Git("config", "--get", "user.name"); got != k.Name {
		t.Errorf("user.name = %q, want %q", got, k.Name)
	}
	if got := repo.Git("config", "--get", "user.email"); got != k.Email {
		t.Errorf("user.email = %q, want %q", got, k.Email)
	}
	if got := repo.Git("config", "--get", "commit.gpgsign"); got != "true" {
		t.Errorf("commit.gpgsign = %q, want %q", got, "true")
	}
}

// TestGPGImport_WritesSigningConfigToTheScopeAsked pins which git config the
// signing settings land in. Repo-local by default; with GitConfigGlobal they
// go to the global gitconfig, which on a shared runner outlives the job.
//
// Nothing exercised the flag, so it could have been ignored in either
// direction unnoticed. Reading with an explicit --local/--global matters:
// plain `config --get` searches every scope and would be satisfied either way.
//
// All four settings are checked, not just the signing key. They are written by
// separate calls through one helper, and the flag is read once per call, so
// "the key went to the right scope" says nothing about where the identity that
// signs with it went. user.email in particular is what a forge matches against
// the key's UID when it decides whether to show a commit as verified.
func TestGPGImport_WritesSigningConfigToTheScopeAsked(t *testing.T) {
	for name, global := range map[string]bool{
		"repo-local by default": false,
		"global when asked":     true,
	} {
		t.Run(name, func(t *testing.T) {
			repo := isolatedgit.NewRepo(t)
			gitRepo := &git.Repo{Dir: repo.Dir}

			k := gpgkey.New(t)

			// isolatedenv points GIT_CONFIG_GLOBAL at /dev/null so a stray
			// global write can never reach the developer's ~/.gitconfig, and
			// gpgkey.New re-applies that after isolatedgit had repointed it.
			// Opt in to a writable one, the way isolatedenv documents, so the
			// --global path has somewhere of our own to land.
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))

			sink := fakeoutputsink.New(t)
			gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{
				GitUserSigningKey: true,
				GitCommitGPGSign:  true,
				GitConfigGlobal:   global,
			}, gitRepo)

			wrote, untouched := "--local", "--global"
			if global {
				wrote, untouched = "--global", "--local"
			}

			// isolatedgit seeds the local scope with its own identity and
			// commit.gpgsign=false, so the other scope is not empty for three
			// of these four keys. Asserting "not the imported value" is the
			// assertion that holds for all of them: a write that leaked into
			// the wrong scope would put the imported value there.
			ctx := context.Background()
			for key, want := range map[string]string{
				"user.signingkey": k.KeyID(),
				"user.name":       k.Name,
				"user.email":      k.Email,
				"commit.gpgsign":  "true",
			} {
				got, err := gitRepo.Run(ctx, "config", wrote, "--get", key)
				if err != nil || got != want {
					t.Errorf("%s %s = %q (err %v), want %q", wrote, key, got, err, want)
				}

				if other, err := gitRepo.Run(ctx, "config", untouched, "--get", key); err == nil && other == want {
					t.Errorf("%s also reached the %s scope: %q", key, untouched, other)
				}
			}
		})
	}
}

func TestGPGImport_DoesNotEnableCommitGPGSignWithoutFlag(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	gitRepo := &git.Repo{Dir: repo.Dir}

	k := gpgkey.New(t)
	sink := fakeoutputsink.New(t)
	gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{GitUserSigningKey: true}, gitRepo)

	if got := repo.Git("config", "--get", "user.signingkey"); got != k.KeyID() {
		t.Errorf("user.signingkey = %q, want %q", got, k.KeyID())
	}
	if got := repo.Git("config", "--get", "commit.gpgsign"); got != "false" {
		t.Errorf("commit.gpgsign = %q, want %q", got, "false")
	}
}

func TestGPGImport_SkipsGitConfigWhenNotRequested(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	gitRepo := &git.Repo{Dir: repo.Dir}

	k := gpgkey.New(t)
	sink := fakeoutputsink.New(t)
	gpgRoundTrip(t, k, sink, apprelease.GPGImportInput{}, gitRepo)

	ctx := context.Background()
	if got, err := gitRepo.Run(ctx, "config", "--get", "user.signingkey"); err == nil {
		t.Errorf("user.signingkey = %q, want missing config", got)
	}
	if got := repo.Git("config", "--get", "user.name"); got != "Test Bot" {
		t.Errorf("user.name = %q, want %q", got, "Test Bot")
	}
	if got := repo.Git("config", "--get", "user.email"); got != "bot@example.invalid" {
		t.Errorf("user.email = %q, want %q", got, "bot@example.invalid")
	}
	if got := repo.Git("config", "--get", "commit.gpgsign"); got != "false" {
		t.Errorf("commit.gpgsign = %q, want %q", got, "false")
	}
}

func TestGPGImport_IsIdempotentOnReimport(t *testing.T) {
	k := gpgkey.New(t)
	armored := k.ArmoredPrivateKey()
	a := adaptergpg.New()
	ctx := context.Background()
	a.DeleteSecretKey(ctx, k.Fingerprint)
	a.DeleteKey(ctx, k.Fingerprint)
	for i := range 2 {
		sink := fakeoutputsink.New(t)
		if _, err := apprelease.GPGImport(ctx, a, openpgp.ReadMetadata, nil, sink,
			apprelease.GPGImportInput{PrivateKey: armored}, &bytes.Buffer{}); err != nil {
			t.Fatalf("GPGImport run %d: %v", i+1, err)
		}
		if got := sink.Single("fingerprint"); got != k.Fingerprint {
			t.Fatalf("run %d fingerprint = %q, want %q", i+1, got, k.Fingerprint)
		}
	}
}

func TestGPGCleanup_RemovesKeyAndIsIdempotent(t *testing.T) {
	k := gpgkey.New(t)
	a := adaptergpg.New()
	ctx := context.Background()

	var log bytes.Buffer
	apprelease.GPGCleanup(ctx, a, k.Fingerprint, &log)

	if !strings.Contains(log.String(), "Removing GPG key") {
		t.Errorf("log missing 'Removing GPG key': %q", log.String())
	}
	// ListKeygrips against a wiped fingerprint must return an error —
	// gpg exits non-zero when --list-secret-keys can't find a match.
	if grips, err := a.ListKeygrips(ctx, k.Fingerprint); err == nil {
		t.Errorf("ListKeygrips after cleanup = %q, want error", grips)
	}

	// Re-running with the same fingerprint must leave it gone. GPGCleanup
	// returns nothing and only reports the adapter's errors, so "it did not blow
	// up" is not evidence of anything -- the keyring is.
	log.Reset()
	apprelease.GPGCleanup(ctx, a, k.Fingerprint, &log)

	if grips, err := a.ListKeygrips(ctx, k.Fingerprint); err == nil {
		t.Errorf("ListKeygrips after the repeated cleanup = %q, want the key still absent", grips)
	}
}

func TestGPGCleanup_NoOpOnEmptyFingerprint(t *testing.T) {
	// GPGCleanup deletes keys. It returns early on an empty fingerprint, so
	// nothing is deleted here, but an isolated GNUPGHOME means that stays
	// true even if that guard ever moves below the delete calls.
	isolatedenv.Isolate(t)

	a := adaptergpg.New()
	var log bytes.Buffer
	apprelease.GPGCleanup(context.Background(), a, "", &log)
	if !strings.Contains(log.String(), "nothing to clean up") {
		t.Errorf("log = %q, want 'nothing to clean up'", log.String())
	}
}
