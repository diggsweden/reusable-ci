// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

func writePinnedWorkflow(t *testing.T, sha string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "release.yml")

	body := "jobs:\n  sign:\n    uses: itiquette/forgejo-ci/.forgejo/workflows/sign.yml@" + sha + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}

	return path
}

func TestPinReachability_ReachablePinPasses(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	reachable := repo.AddCommit("reachable")

	var out bytes.Buffer

	err := appvalidate.PinReachability(context.Background(), &out, appvalidate.PinReachabilityInput{
		Workflows: []string{writePinnedWorkflow(t, reachable)},
		RepoDir:   repo.Dir,
		Main:      "main",
		Subject:   "forgejo-ci",
	})
	if err != nil {
		t.Fatalf("PinReachability: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "reachable") {
		t.Fatalf("expected reachable output, got:\n%s", out.String())
	}
}

func TestPinReachability_OrphanedPinFails(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	reachable := repo.AddCommit("reachable")
	orphan := repo.AddCommit("orphan")
	repo.Git("reset", "--hard", reachable)

	var out bytes.Buffer

	err := appvalidate.PinReachability(context.Background(), &out, appvalidate.PinReachabilityInput{
		Workflows: []string{writePinnedWorkflow(t, orphan)},
		RepoDir:   repo.Dir,
		Main:      "main",
		Subject:   "forgejo-ci",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "ORPHANED") {
		t.Fatalf("expected orphan output, got:\n%s", out.String())
	}
}

func TestPinReachability_TaggedPinPasses(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	reachable := repo.AddCommit("reachable")
	tagged := repo.AddCommit("tagged")
	repo.AddTag("v1.0.0", "release")
	repo.Git("reset", "--hard", reachable)

	var out bytes.Buffer

	err := appvalidate.PinReachability(context.Background(), &out, appvalidate.PinReachabilityInput{
		Workflows: []string{writePinnedWorkflow(t, tagged)},
		RepoDir:   repo.Dir,
		Main:      "main",
		Subject:   "forgejo-ci",
	})
	if err != nil {
		t.Fatalf("PinReachability tagged pin: %v\n%s", err, out.String())
	}
}

func TestPinReachability_NoPinsPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	if err := os.WriteFile(path, []byte("jobs: {}\n"), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}

	var out bytes.Buffer

	err := appvalidate.PinReachability(context.Background(), &out, appvalidate.PinReachabilityInput{Workflows: []string{path}, Subject: "forgejo-ci"})
	if err != nil {
		t.Fatalf("PinReachability no pins: %v", err)
	}

	if !strings.Contains(out.String(), "No forgejo-ci pins") {
		t.Fatalf("expected no-pin output, got:\n%s", out.String())
	}
}
