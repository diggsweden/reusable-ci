// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// fakeCommitPushRepo records the git operations commit-push asks for, so
// the dry-run path can be proven to stop before every mutation.
type fakeCommitPushRepo struct {
	cfg        map[string]string
	added      []string
	hasStaged  bool
	commit     domaingit.CommitInput
	committed  bool
	pushedRef  string
	pushedDest string
}

func (f *fakeCommitPushRepo) Config(_ context.Context, key, value string) error {
	if f.cfg == nil {
		f.cfg = map[string]string{}
	}

	f.cfg[key] = value

	return nil
}

func (f *fakeCommitPushRepo) AddPathspecs(_ context.Context, pathspecs []string) {
	f.added = append(f.added, pathspecs...)
}

func (f *fakeCommitPushRepo) HasStagedChanges(_ context.Context) (bool, error) {
	return f.hasStaged, nil
}

func (f *fakeCommitPushRepo) Commit(_ context.Context, in domaingit.CommitInput) error {
	f.commit = in
	f.committed = true

	return nil
}

func (f *fakeCommitPushRepo) Push(_ context.Context, localRef, remoteBranch string, _ bool, _ runcontext.Credential) error {
	f.pushedRef = localRef
	f.pushedDest = remoteBranch

	return nil
}

func TestCommitPush_DryRunSkipsMutationsAndNarrates(t *testing.T) {
	t.Parallel()

	repo := &fakeCommitPushRepo{hasStaged: true}

	var out bytes.Buffer

	err := appversion.CommitPush(context.Background(), repo, &out, appversion.CommitPushInput{
		Branch:      "main",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore: bump",
		FilePattern: "CHANGELOG.md",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}

	// (a) No config/commit/push mutation is invoked; staging still runs —
	// it is the planning step that decides whether a commit would happen.
	if len(repo.cfg) != 0 {
		t.Errorf("dry-run must not write git config: %v", repo.cfg)
	}

	if repo.committed || repo.pushedRef != "" {
		t.Errorf("dry-run must not commit/push: committed=%v pushed=%q", repo.committed, repo.pushedRef)
	}

	if len(repo.added) != 1 || repo.added[0] != "CHANGELOG.md" {
		t.Errorf("staging (planning) should still run: added=%v", repo.added)
	}

	// (b) Each skipped mutation is narrated.
	for _, want := range []string{
		"[dry-run] skipping git author config (commit is skipped)",
		"[dry-run] would commit staged changes as Test Bot <bot@example.invalid> (signoff)",
		"[dry-run] would push HEAD to origin/main",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q missing narration %q", out.String(), want)
		}
	}
}

func TestCommitPush_DryRunKeepsNoOpDetection(t *testing.T) {
	t.Parallel()

	repo := &fakeCommitPushRepo{hasStaged: false}

	var out bytes.Buffer

	err := appversion.CommitPush(context.Background(), repo, &out, appversion.CommitPushInput{
		Branch:      "main",
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore: bump",
		FilePattern: "CHANGELOG.md",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("CommitPush: %v", err)
	}

	if !strings.Contains(out.String(), "No staged changes") {
		t.Errorf("dry-run should still report the no-op case: %q", out.String())
	}

	if strings.Contains(out.String(), "would commit") {
		t.Errorf("nothing staged: no commit should be previewed: %q", out.String())
	}
}

// TestCommitPush_DryRunStillFailsValidation proves the preview does not
// weaken input validation.
func TestCommitPush_DryRunStillFailsValidation(t *testing.T) {
	t.Parallel()

	err := appversion.CommitPush(context.Background(), &fakeCommitPushRepo{}, &bytes.Buffer{}, appversion.CommitPushInput{
		AuthorName:  "Test Bot",
		AuthorEmail: "bot@example.invalid",
		Message:     "chore: bump",
		FilePattern: "CHANGELOG.md",
		DryRun:      true,
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (missing branch) in dry-run", err)
	}
}

// TestCommitPush_RunsRepositoryHooks records a difference between two
// sibling commit paths.
//
// `version changelog-release` commits with NoHooks and NoVerify set, so
// the consumer's pre-commit and commit-msg hooks do not run. `version
// commit-push` sets neither, and pushes through Repo.Push, which is the
// one push variant that does not pass core.hooksPath=/dev/null --
// PushBranchNoForce and PushTagNoForce both do.
//
// So the shipped version-bump workflow runs the repository's hooks
// during a step that holds a push credential, and the release step next
// to it does not. Recorded rather than changed; see
// docs/open-questions.md.
func TestCommitPush_RunsRepositoryHooks(t *testing.T) {
	repo := &fakeCommitPushRepo{hasStaged: true}

	err := appversion.CommitPush(context.Background(), repo, &bytes.Buffer{}, appversion.CommitPushInput{
		FilePattern: "CHANGELOG.md",
		Message:     "chore: bump",
		AuthorName:  "ci",
		AuthorEmail: "ci@example.com",
		Branch:      "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	// If either of these becomes true, the two paths have been aligned:
	// update this test and drop the open-questions entry.
	if repo.commit.NoHooks {
		t.Error("NoHooks is now set — paths aligned, update docs/open-questions.md")
	}

	if repo.commit.NoVerify {
		t.Error("NoVerify is now set — paths aligned, update docs/open-questions.md")
	}

	// The signoff trailer is the one commit option this path does set.
	if !repo.commit.Signoff {
		t.Error("Signoff not set")
	}
}
