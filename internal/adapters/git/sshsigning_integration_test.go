//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// The SSH allowed-signers path is a trust boundary: ok=true means the tag
// is signed by a key the repository authorises. Until now it was only
// ever exercised through fakes at the app layer, and a fake decides the
// answer it is testing. In particular the denial is detected by matching
// git's own text --
//
//	strings.Contains(msg, "No principal matched")
//
// -- and nothing had ever compared that string against what git actually
// prints. These tests drive the real adapter against a real ssh-signed
// tag, so the three outcomes the orchestrator distinguishes (authorised,
// denied, broken) are separated by real output.
//
// No t.Parallel(): isolatedgit.NewRepo scrubs the environment with
// t.Setenv.

// signingRepo returns an isolated repo configured to sign tags with a
// fresh ed25519 key, plus the public key line for allowed_signers.
func signingRepo(t *testing.T) (*isolatedgit.Repo, string) {
	t.Helper()

	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}

	repo := isolatedgit.NewRepo(t)
	keyPath := filepath.Join(repo.Home, "signing_key")

	//nolint:gosec // fixed arguments, test-owned paths.
	out, err := exec.CommandContext(t.Context(), "ssh-keygen",
		"-t", "ed25519", "-N", "", "-C", "bot@example.invalid", "-f", keyPath).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	repo.Git("config", "gpg.format", "ssh")
	repo.Git("config", "user.signingkey", keyPath)

	return repo, strings.TrimSpace(string(pub))
}

// writeAllowedSigners writes an allowed_signers file authorising
// principal for the given public key line.
func writeAllowedSigners(t *testing.T, dir, principal, pubKey string) string {
	t.Helper()

	path := filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(path, []byte(principal+" "+pubKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestVerifyTagSSHAgainstAllowedSigners_AuthorisedSigner(t *testing.T) {
	repo, pubKey := signingRepo(t)
	repo.Git("tag", "-s", "v1.0.0", "-m", "release v1.0.0")

	allowed := writeAllowedSigners(t, repo.Home, "bot@example.invalid", pubKey)

	ok, out, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSSHAgainstAllowedSigners(context.Background(), "v1.0.0", allowed)
	if err != nil {
		t.Fatalf("a tag signed by an allow-listed key was not accepted: %v\n%s", err, out)
	}

	if !ok {
		t.Errorf("ok = false for an allow-listed signer\n%s", out)
	}
}

// TestVerifyTagSSHAgainstAllowedSigners_SignerNotAllowListed is the case
// the whole mechanism exists for, and the one the string match decides.
// The tag carries a perfectly valid signature; the key just is not
// authorised. That has to come back as ErrPermissionDenied -- the
// orchestrator maps it to exit 77 -- and not as a generic failure.
func TestVerifyTagSSHAgainstAllowedSigners_SignerNotAllowListed(t *testing.T) {
	repo, _ := signingRepo(t)
	repo.Git("tag", "-s", "v1.0.0", "-m", "release v1.0.0")

	// A different key entirely: valid file, valid principal, wrong key.
	otherKey := filepath.Join(repo.Home, "other_key")

	//nolint:gosec // fixed arguments, test-owned paths.
	if out, err := exec.CommandContext(t.Context(), "ssh-keygen",
		"-t", "ed25519", "-N", "", "-C", "other@example.invalid", "-f", otherKey).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	otherPub, err := os.ReadFile(otherKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	allowed := writeAllowedSigners(t, repo.Home, "bot@example.invalid", strings.TrimSpace(string(otherPub)))

	ok, out, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSSHAgainstAllowedSigners(context.Background(), "v1.0.0", allowed)
	if ok {
		t.Fatalf("a tag signed by a key outside allowed_signers was accepted\n%s", out)
	}

	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied so the caller can exit 77.\n"+
			"git said:\n%s", err, out)
	}
}

// TestVerifyTagSSHAgainstAllowedSigners_BrokenSetupIsNotADenial keeps the
// two failure kinds apart. Neither of these is "signer not authorised",
// so neither may claim ErrPermissionDenied: reporting a missing file or
// an unsigned tag as a denial would tell an operator their key is not
// trusted when the real problem is elsewhere.
func TestVerifyTagSSHAgainstAllowedSigners_BrokenSetupIsNotADenial(t *testing.T) {
	repo, pubKey := signingRepo(t)
	repo.Git("tag", "-s", "v1.0.0", "-m", "release v1.0.0")
	repo.Git("tag", "-a", "v0.9.0", "-m", "unsigned annotated tag")

	allowed := writeAllowedSigners(t, repo.Home, "bot@example.invalid", pubKey)
	adapter := &adaptergit.Repo{Dir: repo.Dir}

	for _, tc := range []struct {
		name    string
		tag     string
		signers string
	}{
		{name: "tag is annotated but unsigned", tag: "v0.9.0", signers: allowed},
		{name: "tag does not exist", tag: "v4.0.0", signers: allowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, out, err := adapter.VerifyTagSSHAgainstAllowedSigners(context.Background(), tc.tag, tc.signers)
			if ok || err == nil {
				t.Fatalf("got (ok=%v, err=%v), want a refusal\n%s", ok, err, out)
			}

			if errors.Is(err, errs.ErrPermissionDenied) {
				t.Errorf("reported as a signer denial, which it is not: %v\n%s", err, out)
			}
		})
	}
}

// TestVerifyTagSSHAgainstAllowedSigners_MissingFileReadsAsADenial records
// a gap between the adapter's documented contract and its behaviour. The
// doc comment says err distinguishes "the tag isn't signed, isn't
// annotated, the file is missing, or git itself errored"; the file being
// missing is in fact reported as ErrPermissionDenied -- the sentinel that
// means "signed, but this signer is not authorised".
//
// The cause is that git prints "No principal matched." for a missing
// file, an empty file and a genuinely unauthorised key alike, and the
// match looks for nothing else. It could: a missing file additionally
// prints "Unable to open allowed keys file".
//
// It is unreachable and fail-closed. Both callers stat the path before
// they get here -- checkSSHSignerAllowlist in app/validate/tags.go and
// requireNonEmptyFile in app/release/verifyreleaserequest.go -- so the
// operator is told the file is missing rather than that their key is
// untrusted. The gap is that the adapter reads as though it made that
// distinction itself.
//
// Recorded in docs/open-questions.md ("A missing allowed_signers file is
// reported as a signer denial").
func TestVerifyTagSSHAgainstAllowedSigners_MissingFileReadsAsADenial(t *testing.T) {
	repo, _ := signingRepo(t)
	repo.Git("tag", "-s", "v1.0.0", "-m", "release v1.0.0")

	_, out, err := (&adaptergit.Repo{Dir: repo.Dir}).
		VerifyTagSSHAgainstAllowedSigners(context.Background(), "v1.0.0", filepath.Join(repo.Home, "absent"))
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("the adapter now separates a missing file from a denial (err = %v) -- "+
			"close the open question and delete this test", err)
	}

	// The output git returned does carry the distinction, which is what
	// makes the fix a one-line addition to the match.
	if !strings.Contains(out, "Unable to open allowed keys file") {
		t.Errorf("git no longer reports the missing file separately, so the fix would need another signal:\n%s", out)
	}
}
