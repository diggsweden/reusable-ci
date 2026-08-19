// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

// realPinGit drives the checks against a real git binary, mirroring
// newRealGit in tags_test.go: these tests assert reachability semantics, so
// a fake would only restate the answer we are trying to verify.
type realPinGit struct{}

func (realPinGit) Open(dir string) appvalidate.PinGitOps {
	return &adaptergit.Repo{Dir: dir}
}

func (realPinGit) Clone(ctx context.Context, remote, dir string) error {
	_, err := adaptergit.New().Run(ctx, "clone", "--quiet", remote, dir)

	return err
}

func writePinnedWorkflow(t *testing.T, sha string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "release.yml")

	body := "jobs:\n  sign:\n    uses: itiquette/forgejo-ci/.forgejo/workflows/sign.yml@" + sha + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}

	return path
}

// writeWorkflowWithPins writes one workflow referencing several pinned
// SHAs, for the cases where more than one pin must be checked.
func writeWorkflowWithPins(t *testing.T, name string, shas ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	var body strings.Builder

	body.WriteString("jobs:\n")

	for i, sha := range shas {
		fmt.Fprintf(&body, "  job%d:\n    uses: itiquette/forgejo-ci/.forgejo/workflows/sign.yml@%s\n", i, sha)
	}

	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}

	return path
}

// TestPinReachability_ChecksEveryPinInEveryFile covers the two loops.
// Every test here passes one workflow holding one pin, so a check that
// stopped at the first file, or the first pin within a file, would
// satisfy all of them -- while an orphaned pin anywhere else went
// unreported. An orphaned pin is a supply-chain hazard precisely because
// it is not reachable from any branch or tag: nothing stops it being
// rewritten or garbage-collected out from under the workflow that
// depends on it.
func TestPinReachability_ChecksEveryPinInEveryFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, reachable, orphan string) []string
	}{
		{
			name: "orphan in a later file",
			build: func(t *testing.T, reachable, orphan string) []string {
				t.Helper()

				return []string{
					writeWorkflowWithPins(t, "a.yml", reachable),
					writeWorkflowWithPins(t, "b.yml", orphan),
				}
			},
		},
		{
			name: "orphan is the second pin in one file",
			build: func(t *testing.T, reachable, orphan string) []string {
				t.Helper()

				return []string{writeWorkflowWithPins(t, "a.yml", reachable, orphan)}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := isolatedgit.NewRepo(t)
			reachable := repo.AddCommit("reachable")
			orphan := repo.AddCommit("orphan")
			repo.Git("reset", "--hard", reachable)

			var out bytes.Buffer

			err := appvalidate.PinReachability(context.Background(), realPinGit{}, &out, appvalidate.PinReachabilityInput{
				Workflows: tc.build(t, reachable, orphan),
				RepoDir:   repo.Dir,
				Main:      "main",
				Subject:   "forgejo-ci",
			})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation\n%s", err, out.String())
			}

			// And it names the orphan, not merely that something failed.
			if !strings.Contains(out.String(), orphan) {
				t.Errorf("output does not name the orphaned pin %s:\n%s", orphan, out.String())
			}
		})
	}
}

func TestPinReachability_ReachablePinPasses(t *testing.T) {
	repo := isolatedgit.NewRepo(t)
	reachable := repo.AddCommit("reachable")

	var out bytes.Buffer

	err := appvalidate.PinReachability(context.Background(), realPinGit{}, &out, appvalidate.PinReachabilityInput{
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

	err := appvalidate.PinReachability(context.Background(), realPinGit{}, &out, appvalidate.PinReachabilityInput{
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

	err := appvalidate.PinReachability(context.Background(), realPinGit{}, &out, appvalidate.PinReachabilityInput{
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

	err := appvalidate.PinReachability(context.Background(), realPinGit{}, &out, appvalidate.PinReachabilityInput{Workflows: []string{path}, Subject: "forgejo-ci"})
	if err != nil {
		t.Fatalf("PinReachability no pins: %v", err)
	}

	if !strings.Contains(out.String(), "No forgejo-ci pins") {
		t.Fatalf("expected no-pin output, got:\n%s", out.String())
	}
}
