//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	adaptergpg "github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/internal/adapters/openpgp"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/gpgkey"
	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedgit"
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

	var out bytes.Buffer
	md, err := apprelease.GPGImport(ctx, a, openpgp.ReadMetadata, gitRepo, sink, in, &out)
	if err != nil {
		t.Fatalf("GPGImport: %v", err)
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
	sink := fakeoutputsink.New(t)
	_, err := apprelease.GPGImport(context.Background(), adaptergpg.New(), openpgp.ReadMetadata, nil, sink,
		apprelease.GPGImportInput{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "PrivateKey is required") {
		t.Errorf("err = %v, want PrivateKey-required error", err)
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

	// Re-running with the same fingerprint must not panic / error-propagate.
	log.Reset()
	apprelease.GPGCleanup(ctx, a, k.Fingerprint, &log)
}

func TestGPGCleanup_NoOpOnEmptyFingerprint(t *testing.T) {
	a := adaptergpg.New()
	var log bytes.Buffer
	apprelease.GPGCleanup(context.Background(), a, "", &log)
	if !strings.Contains(log.String(), "nothing to clean up") {
		t.Errorf("log = %q, want 'nothing to clean up'", log.String())
	}
}
