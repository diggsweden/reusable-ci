// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

const (
	rerunRequestTag = "release-request/v1.0.0"
	rerunFinalTag   = "v1.0.0"
)

// historyGit is a linear history with distinct commits for the request tag,
// the final tag, the final tag's parent and the branch head. Only the refs a
// fixture names resolve, and each tag verifies only when the fixture says so.
type historyGit struct {
	stubGit

	history    []string          // oldest first
	refs       map[string]string // full revision → commit
	verifies   map[string]bool   // tag → signature verifies
	finalTagAt string            // "" when the final tag does not exist

	mu    sync.Mutex
	asked []string
}

func (h *historyGit) RevParse(_ context.Context, ref string) (string, error) {
	h.record("RevParse " + ref)

	if commit, ok := h.refs[ref]; ok {
		return commit, nil
	}

	return "", fmt.Errorf("unknown revision %s: %w", ref, errs.ErrValidation)
}

func (h *historyGit) TagsPointingAt(_ context.Context, commit string) ([]string, error) {
	var tags []string

	for ref, at := range h.refs {
		if name, ok := strings.CutPrefix(ref, "refs/tags/"); ok && at == commit && strings.HasSuffix(name, "^{commit}") {
			tags = append(tags, strings.TrimSuffix(name, "^{commit}"))
		}
	}

	slices.Sort(tags)

	return tags, nil
}

func (h *historyGit) IsAncestor(_ context.Context, ancestor, descendant string) (bool, error) {
	return slices.Index(h.history, ancestor) <= slices.Index(h.history, descendant) && slices.Contains(h.history, ancestor), nil
}

func (h *historyGit) VerifyTagSignature(_ context.Context, tag string, armor []byte) (string, string, bool, error) {
	h.record("VerifyTagSignature " + tag)

	if len(armor) == 0 || !h.verifies[tag] {
		return "", "", false, nil
	}

	return "x <x@y>", "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD", true, nil
}

func (h *historyGit) TagExists(_ context.Context, tag string) (bool, error) {
	h.record("TagExists " + tag)

	return tag == rerunFinalTag && h.finalTagAt != "", nil
}

func (h *historyGit) record(call string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.asked = append(h.asked, call)
}

func (h *historyGit) calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.asked)
}

// newHistoryGit places the request tag at c1, and the final tag (with its
// parent) and the remote branch head where the fixture says, on history.
func newHistoryGit(history []string, finalParent, final, head string) *historyGit {
	refs := map[string]string{
		"refs/tags/" + rerunRequestTag + "^{commit}": "c1",
		"refs/remotes/origin/main^{commit}":          head,
	}

	if final != "" {
		refs["refs/tags/"+rerunFinalTag+"^{commit}"] = final
		refs["refs/tags/"+rerunFinalTag+"^{commit}^"] = finalParent
	}

	return &historyGit{
		history:    history,
		refs:       refs,
		verifies:   map[string]bool{rerunRequestTag: true, rerunFinalTag: true},
		finalTagAt: final,
	}
}

func runRerunPrerequisites(t *testing.T, repo *historyGit, mutate func(*appvalidate.PrerequisitesInput)) (appvalidate.PrerequisitesResult, error) {
	t.Helper()

	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithBotPermissions(provider.BotPermissions{
		UserAccessible: true, RepoAccessible: true, BranchesAccessible: true,
	})

	in := appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic token and key markers; nothing real is read.
		RefType:             "tag",
		Tag:                 rerunRequestTag,
		Ref:                 "refs/tags/" + rerunRequestTag,
		Branch:              "main",
		ReleaseToken:        "github_pat_AAAA",
		Repository:          "owner/repo",
		ReleaseGPGPublicKey: "test-key",
	}

	if mutate != nil {
		mutate(&in)
	}

	return appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{GitRepo: repo, Provider: fp}, io.Discard, output.Annotator{}, in)
}

// outcome finds the named check, failing when it was not recorded at all, so
// a check that silently never ran cannot pass as "not failed".
func outcome(t *testing.T, result appvalidate.PrerequisitesResult, name string) appvalidate.ValidatorOutcome {
	t.Helper()

	for _, check := range result.Checks {
		if check.Name == name {
			return check
		}
	}

	t.Fatalf("check %s not recorded; checks = %+v", name, result.Checks)

	return appvalidate.ValidatorOutcome{}
}

// TestPrerequisites_FinalTagRerunStates distinguishes the request commit, the
// final tag's commit and parent, and the branch head. An exact rerun (the
// final tag's commit is the bump on the request commit, at the branch head and
// signed) passes and reports the final commit. A final tag released from a
// different request commit is refused; it used to be accepted whenever it sat
// at the head and verified, whatever commit the human had signed. A final tag
// behind the head, or one that does not verify, is refused on its own, while
// the request's own checks still pass. Without a final tag the rerun is
// skipped and the request must be the branch head.
func TestPrerequisites_FinalTagRerunStates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		repo       func() *historyGit
		wantRerun  error // nil: passes
		wantText   string
		wantSkip   bool
		wantCommit error // tag-commit class for the request
		wantSHA    string
	}{
		{
			name:    "exact rerun",
			repo:    func() *historyGit { return newHistoryGit([]string{"c0", "c1", "c2"}, "c1", "c2", "c2") },
			wantSHA: "c2",
		},
		{
			name:      "final tag released from another request commit",
			repo:      func() *historyGit { return newHistoryGit([]string{"c0", "c1", "c1x", "c2"}, "c1x", "c2", "c2") },
			wantRerun: errs.ErrValidation,
			wantText:  "was released from c1x, not from release request " + rerunRequestTag + " at c1",
			wantSHA:   "c2",
		},
		{
			name:      "final tag behind the branch head",
			repo:      func() *historyGit { return newHistoryGit([]string{"c0", "c1", "c2", "c3"}, "c1", "c2", "c3") },
			wantRerun: errs.ErrValidation,
			wantText:  "is not branch HEAD",
			wantSHA:   "c2",
		},
		{
			name: "final tag does not verify",
			repo: func() *historyGit {
				repo := newHistoryGit([]string{"c0", "c1", "c2"}, "c1", "c2", "c2")
				repo.verifies[rerunFinalTag] = false

				return repo
			},
			wantRerun: errs.ErrPermissionDenied,
			wantText:  "could not be verified",
			wantSHA:   "c2",
		},
		{
			name:     "no final tag",
			repo:     func() *historyGit { return newHistoryGit([]string{"c0", "c1"}, "", "", "c1") },
			wantSkip: true,
		},
		{
			name:       "no final tag and the request is behind the head",
			repo:       func() *historyGit { return newHistoryGit([]string{"c0", "c1", "c2"}, "", "", "c2") },
			wantSkip:   true,
			wantCommit: errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := runRerunPrerequisites(t, tc.repo(), nil)

			if result.ExistingReleaseSHA != tc.wantSHA {
				t.Errorf("ExistingReleaseSHA = %q, want %q", result.ExistingReleaseSHA, tc.wantSHA)
			}

			rerun := outcome(t, result, "final-tag-rerun")
			if rerun.Skipped != tc.wantSkip {
				t.Fatalf("final-tag-rerun skipped = %t, want %t: %+v", rerun.Skipped, tc.wantSkip, rerun)
			}

			assertCheckErr(t, "final-tag-rerun", rerun.Err, tc.wantRerun)

			if tc.wantText != "" && (rerun.Err == nil || !strings.Contains(rerun.Err.Error(), tc.wantText)) {
				t.Errorf("final-tag-rerun err = %v, want it to say %q", rerun.Err, tc.wantText)
			}

			assertCheckErr(t, "tag-commit", outcome(t, result, "tag-commit").Err, tc.wantCommit)

			for _, name := range []string{"ref-type", "tag-format", "tag-uniqueness", "tag-signature", "release-token", "bot-permissions"} {
				if check := outcome(t, result, name); check.Skipped || check.Err != nil {
					t.Errorf("%s = %+v, want a passing check independent of the final tag", name, check)
				}
			}

			if wantFailure := tc.wantRerun != nil || tc.wantCommit != nil; (err != nil) != wantFailure {
				t.Errorf("aggregate err = %v, want failure %t", err, wantFailure)
			}
		})
	}
}

func assertCheckErr(t *testing.T, name string, got, want error) {
	t.Helper()

	if want == nil && got != nil {
		t.Errorf("%s err = %v, want pass", name, got)
	}

	if want != nil && !errors.Is(got, want) {
		t.Errorf("%s err = %v, want %v", name, got, want)
	}
}

// TestPrerequisites_EachEnabledPolicyRunsItsOwnCheck turns on one policy flag
// at a time with inputs its check refuses. The check it gates must be
// recorded as run with its own refusal, and every other gated check must stay
// skipped: a flag that was ignored would leave its check skipped, and one that
// was wired to the wrong check would change a different row.
func TestPrerequisites_EachEnabledPolicyRunsItsOwnCheck(t *testing.T) {
	t.Parallel()

	gated := []string{"gpg-public-key", "maven-central", "cargo", "jvm-reproducibility"}

	for _, tc := range []struct {
		check  string
		enable func(*appvalidate.PrerequisitesInput)
		want   error
	}{
		{"gpg-public-key", func(in *appvalidate.PrerequisitesInput) { in.RequiresGPGSigning, in.ReleaseGPGPublicKey = true, "" }, errs.ErrPermissionDenied},
		{"maven-central", func(in *appvalidate.PrerequisitesInput) { in.HasMavenCentralTarget = true }, errs.ErrPermissionDenied},
		{"cargo", func(in *appvalidate.PrerequisitesInput) { in.HasCargoTarget = true }, errs.ErrUsage},
		{"jvm-reproducibility", func(in *appvalidate.PrerequisitesInput) { in.HasJVMTarget = true }, errs.ErrUsage},
	} {
		t.Run(tc.check, func(t *testing.T) {
			t.Parallel()

			repo := newHistoryGit([]string{"c0", "c1"}, "", "", "c1")

			fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithBotPermissions(provider.BotPermissions{
				UserAccessible: true, RepoAccessible: true, BranchesAccessible: true,
			})

			in := appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic token and key markers; nothing real is read.
				RefType: "tag", Tag: rerunRequestTag, Ref: "refs/tags/" + rerunRequestTag, Branch: "main",
				ReleaseToken: "github_pat_AAAA", Repository: "owner/repo", ReleaseGPGPublicKey: "test-key",
			}
			tc.enable(&in)

			result, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{GitRepo: repo, Provider: fp, Cargo: fakeCargoTool{version: "cargo 1.90.0"}}, io.Discard, output.Annotator{}, in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("aggregate err = %v, want %v", err, tc.want)
			}

			for _, name := range gated {
				check := outcome(t, result, name)

				if name == tc.check {
					if check.Skipped || !errors.Is(check.Err, tc.want) {
						t.Errorf("%s = %+v, want it run and refused with %v", name, check, tc.want)
					}

					continue
				}

				if !check.Skipped {
					t.Errorf("%s = %+v, want it skipped while its flag is off", name, check)
				}
			}
		})
	}
}

// TestPrerequisites_RefusedBeforeAnyCheck covers the inputs refused before any
// check runs or git is asked anything: a ref type that is not exactly "tag"
// no longer runs the tag suite and its git reads for a trigger the ref-type
// check then refuses, and a Cargo target without a Cargo tool is a usage error
// instead of a panic inside a check goroutine.
func TestPrerequisites_RefusedBeforeAnyCheck(t *testing.T) {
	t.Parallel()

	t.Run("ref type in another case", func(t *testing.T) {
		t.Parallel()

		repo := newHistoryGit([]string{"c0", "c1"}, "c1", "c1", "c1")

		result, err := runRerunPrerequisites(t, repo, func(in *appvalidate.PrerequisitesInput) { in.RefType = "TAG" })
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want the ref-type refusal", err)
		}

		if check := outcome(t, result, "ref-type"); !errors.Is(check.Err, errs.ErrValidation) {
			t.Errorf("ref-type = %+v, want refused", check)
		}

		for _, name := range []string{"tag-format", "tag-uniqueness", "tag-commit", "tag-signature", "final-tag-rerun"} {
			if check := outcome(t, result, name); !check.Skipped {
				t.Errorf("%s = %+v, want skipped for a non-tag trigger", name, check)
			}
		}

		if calls := repo.calls(); len(calls) != 0 {
			t.Errorf("git asked %v for a refused trigger", calls)
		}
	})

	t.Run("cargo target without a cargo tool", func(t *testing.T) {
		t.Parallel()

		repo := newHistoryGit([]string{"c0", "c1"}, "", "", "c1")

		result, err := runRerunPrerequisites(t, repo, func(in *appvalidate.PrerequisitesInput) { in.HasCargoTarget = true })
		if !errors.Is(err, errs.ErrUsage) || len(result.Checks) != 0 {
			t.Fatalf("result = %+v err = %v, want a usage error and no checks", result, err)
		}

		if calls := repo.calls(); len(calls) != 0 {
			t.Errorf("git asked %v before the missing dependency was refused", calls)
		}
	})
}
