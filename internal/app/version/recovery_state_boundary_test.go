// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

var errRecoveryBoundary = errors.New("owned recovery dependency failure")

type recoveryFailureRepo struct {
	fakeChangelogRenderGit
	fail string
}

func (r *recoveryFailureRepo) FetchBranch(ctx context.Context, remote, branch string, cred runcontext.Credential) error {
	if r.fail == "branch" {
		return errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.FetchBranch(ctx, remote, branch, cred)
}
func (r *recoveryFailureRepo) RemoteTagCommitIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error) {
	if r.fail == "lookup" {
		return "", false, errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.RemoteTagCommitIfExists(ctx, remote, tag, cred)
}
func (r *recoveryFailureRepo) FetchTagForceFromRemote(ctx context.Context, remote, tag string, cred runcontext.Credential) error {
	if r.fail == "fetch" {
		return errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.FetchTagForceFromRemote(ctx, remote, tag, cred)
}
func (r *recoveryFailureRepo) RevParse(ctx context.Context, ref string) (string, error) {
	if r.fail == "resolve" {
		return "", errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.RevParse(ctx, ref)
}
func (r *recoveryFailureRepo) CommitSubject(ctx context.Context, ref string) (string, error) {
	if r.fail == "subject" {
		return "", errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.CommitSubject(ctx, ref)
}
func (r *recoveryFailureRepo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	if r.fail == "ancestor" {
		return false, errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.IsAncestor(ctx, a, b)
}
func (r *recoveryFailureRepo) Checkout(ctx context.Context, ref string) error {
	if r.fail == "checkout" {
		return errRecoveryBoundary
	}

	return r.fakeChangelogRenderGit.Checkout(ctx, ref)
}

func TestRecoveryStateBoundary_AllFailuresPreserveMarker(t *testing.T) { //nolint:gocognit // every recovery stage faces both an absent and a preexisting marker.
	for _, state := range []string{"absent", "existing"} {
		for _, stage := range []string{"branch", "lookup", "fetch", "resolve", "subject", "ancestor", "checkout", "marker"} {
			t.Run(state+"/"+stage, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)

				marker := filepath.Join(root, ".existing-release-sha")
				if state == "existing" {
					if err := os.WriteFile(marker, []byte("original marker\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				repo := &recoveryFailureRepo{fakeChangelogRenderGit: fakeChangelogRenderGit{remoteTagExists: true, remoteTagCommit: "0123456789abcdef0123456789abcdef01234567", subject: "chore(release): bump to v1.2.3", ancestor: true}, fail: stage}
				if stage == "marker" {
					if os.Geteuid() == 0 {
						t.Skip("permission refusal requires unprivileged ownership")
					}

					if err := os.Chmod(root, 0o500); err != nil { //nolint:gosec // owned directory keeps traversal but refuses marker creation.
						t.Fatal(err)
					}

					t.Cleanup(func() {
						if err := os.Chmod(root, 0o700); err != nil { //nolint:gosec // restore the owned directory for test cleanup.
							t.Error(err)
						}
					})
				}

				var out bytes.Buffer

				renderer := &fakeChangelogRenderer{}

				_, err := appversion.ChangelogRender(t.Context(), repo, renderer, &out, appversion.ChangelogRenderInput{Tag: "v1.2.3", RepositoryURL: "https://forge.example/owner/repo", ChangelogConfig: "full", CommitBodyConfig: "body", ExistingReleaseSHAPath: marker})
				if err == nil || (stage != "marker" && !errors.Is(err, errRecoveryBoundary)) || out.Len() != 0 || renderer.fullTag != "" {
					t.Fatalf("stage=%s err=%v out=%s", stage, err, &out)
				}

				body, readErr := os.ReadFile(marker)
				if state == "absent" {
					if !errors.Is(readErr, os.ErrNotExist) {
						t.Fatalf("premature marker: %q %v", body, readErr)
					}
				} else {
					info, statErr := os.Stat(marker)
					if readErr != nil || statErr != nil || string(body) != "original marker\n" || info.Mode().Perm() != 0o600 {
						t.Fatalf("changed marker: %q %v %v", body, readErr, statErr)
					}
				}
			})
		}
	}
}

func TestTagObjectBoundary_AnnotatedInBothModes(t *testing.T) { //nolint:gocognit // positive and negative object types independently cover signed and unsigned recovery.
	t.Parallel()

	for _, signed := range []bool{false, true} {
		for _, kind := range []string{"tag", "commit", "type failure"} {
			repo := &fakeTagReleaseRepo{exists: true, localSHA: headSHA, tagType: kind}
			if kind == "type failure" {
				repo.typeErr = errRecoveryBoundary
			}

			if !signed {
				repo.signatureErr = errRecoveryBoundary
			}

			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			_, err := appversion.TagRelease(t.Context(), repo, appversion.TagReleaseInput{Tag: "v3.5.7", Signed: signed}, sink, &out)
			if !slices.Equal(repo.typeRefs, []string{"refs/tags/v3.5.7"}) {
				t.Fatalf("type lookup=%v", repo.typeRefs)
			}

			if kind == "tag" { //nolint:nestif // the successful type has distinct signature expectations in each mode.
				if err != nil || repo.pushed != "v3.5.7" || repo.created != "" {
					t.Fatalf("annotated rerun err=%v repo=%+v", err, repo)
				}

				want := 0
				if signed {
					want = 1
				}

				if len(repo.verified) != want {
					t.Fatalf("signed=%v verified=%v", signed, repo.verified)
				}
			} else {
				cause := errs.ErrValidation
				if kind == "type failure" {
					cause = errRecoveryBoundary
				}

				if !errors.Is(err, cause) || len(repo.verified) != 0 || repo.pushed != "" || repo.created != "" || len(sink.Keys()) != 0 || out.Len() != 0 {
					t.Fatalf("kind=%s err=%v repo=%+v", kind, err, repo)
				}
			}
		}
	}
}
