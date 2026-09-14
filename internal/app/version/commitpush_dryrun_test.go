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
	cfg         map[string]string
	added       []string
	hasStaged   bool
	commit      domaingit.CommitInput
	committed   bool
	pushedRef   string
	pushedDest  string
	remoteSHA   string
	remoteSeen  bool
	preStaged   bool
	indexErr    error
	localSHA    string
	statusPaths []string
	leasedPush  domaingit.BranchPushInput
}

func (f *fakeCommitPushRepo) CheckOriginPushDestination(_ context.Context) error {
	return nil
}

func (f *fakeCommitPushRepo) HasPathspecChanges(_ context.Context, paths []string) (bool, error) {
	f.statusPaths = append([]string{}, paths...)

	return f.hasStaged, f.indexErr
}
func (f *fakeCommitPushRepo) RevParse(_ context.Context, _ string) (string, error) {
	if f.committed {
		return strings.Repeat("c", 40), nil
	}

	return f.localSHA, nil
}

func (f *fakeCommitPushRepo) CommitParents(_ context.Context, _ string) ([]string, error) {
	return []string{f.localSHA}, nil
}

func (f *fakeCommitPushRepo) PushCommitWithLease(_ context.Context, in domaingit.BranchPushInput, _ runcontext.Credential) error {
	f.leasedPush = in
	f.pushedRef, f.pushedDest = in.CommitSHA, in.Branch

	return nil
}

func (f *fakeCommitPushRepo) RemoteBranchCommit(_ context.Context, _, _ string, _ runcontext.Credential) (string, bool, error) {
	return f.remoteSHA, f.remoteSeen, nil
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
	return f.preStaged || (len(f.added) > 0 && f.hasStaged), f.indexErr
}

func TestCommitPush_RefusesPreexistingIndexBeforeMutations(t *testing.T) {
	t.Parallel()

	for _, dry := range []bool{false, true} {
		repo := &fakeCommitPushRepo{preStaged: true, hasStaged: true}

		var out bytes.Buffer

		err := appversion.CommitPush(t.Context(), repo, &out, appversion.CommitPushInput{Branch: "main", AuthorName: "fixture", AuthorEmail: "fixture@example.invalid", Message: "update", FilePattern: "CHANGELOG.md", DryRun: dry})
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("err=%v, want ErrValidation", err)
		}

		if len(repo.cfg) != 0 || len(repo.added) != 0 || repo.committed || repo.pushedRef != "" || out.Len() != 0 {
			t.Error("preexisting index caused mutations or success output")
		}
	}
}

func TestCommitPush_IndexReadErrorStopsBeforeMutation(t *testing.T) {
	t.Parallel()

	repo := &fakeCommitPushRepo{indexErr: errs.ErrPermissionDenied}

	err := appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, appversion.CommitPushInput{Branch: "main", AuthorName: "fixture", AuthorEmail: "fixture@example.invalid", Message: "update", FilePattern: "CHANGELOG.md"})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("err=%v, want original cause", err)
	}

	if len(repo.cfg) != 0 || len(repo.added) != 0 || repo.committed || repo.pushedRef != "" {
		t.Error("index error did not stop mutation")
	}
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

	// No config, index, commit or push mutation is invoked by a preview.
	if len(repo.cfg) != 0 {
		t.Errorf("dry-run must not write git config: %v", repo.cfg)
	}

	if repo.committed || repo.pushedRef != "" {
		t.Errorf("dry-run must not commit/push: committed=%v pushed=%q", repo.committed, repo.pushedRef)
	}

	if len(repo.added) != 0 || len(repo.statusPaths) != 1 || repo.statusPaths[0] != "CHANGELOG.md" {
		t.Errorf("preview must inspect without staging: added=%v status=%v", repo.added, repo.statusPaths)
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

func TestCommitPush_NoOpDoesNotConfigureIdentity(t *testing.T) {
	t.Parallel()

	repo := &fakeCommitPushRepo{}

	err := appversion.CommitPush(t.Context(), repo, &bytes.Buffer{}, appversion.CommitPushInput{Branch: "main", AuthorName: "fixture", AuthorEmail: "fixture@example.invalid", Message: "update", FilePattern: "CHANGELOG.md"})
	if err != nil {
		t.Fatal(err)
	}

	if len(repo.cfg) != 0 || repo.committed || repo.pushedRef != "" {
		t.Fatal("no-op changed identity or refs")
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

func TestCommitPush_DisablesRepositoryHooks(t *testing.T) {
	t.Parallel()

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

	if !repo.commit.NoHooks {
		t.Error("NoHooks not set")
	}

	// The signoff trailer is the one commit option this path does set.
	if !repo.commit.Signoff {
		t.Error("Signoff not set")
	}
}

func TestCommitPush_RefusesMovedAuthorizedBranchBeforeMutation(t *testing.T) {
	t.Parallel()

	repo := &fakeCommitPushRepo{hasStaged: true, localSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", remoteSeen: true, remoteSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}

	err := appversion.CommitPush(context.Background(), repo, &bytes.Buffer{}, appversion.CommitPushInput{
		FilePattern: "CHANGELOG.md",
		Message:     "chore: bump",
		AuthorName:  "ci",
		AuthorEmail: "ci@example.com",
		Branch:      "main",
		ExpectedSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if len(repo.cfg) != 0 || len(repo.added) != 0 || repo.committed || repo.pushedRef != "" {
		t.Fatalf("moved branch caused mutation: %+v", repo)
	}
}
