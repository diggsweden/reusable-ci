// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/stretchr/testify/require"
)

var (
	errVerifyTagHEAD           = errors.New("HEAD unavailable")
	errVerifyTagType           = errors.New("tag type unavailable")
	errVerifyTagLocalObject    = errors.New("local object unavailable")
	errVerifyTagRemoteObject   = errors.New("remote object unavailable")
	errVerifyTagRemoteCommit   = errors.New("remote commit unavailable")
	errVerifyTagRemoteVersions = errors.New("remote versions unavailable")
)

// fakeTagGit implements the tag-verify git surface.
type fakeTagGit struct {
	head         string
	tagCommit    string
	tags         []string
	tagType      string
	localObject  string
	remoteObject string
	queries      *int
	repositories *[]string
	observe      func(context.Context, tagVerifyCall) error
}

type tagVerifyCall struct {
	method     string
	ref        string
	repository string
	token      runcontext.Credential
}

func (f fakeTagGit) RevParse(ctx context.Context, ref string) (string, error) {
	err := f.record(ctx, tagVerifyCall{method: "RevParse", ref: ref})
	if ref == "HEAD" {
		return f.head, err
	}

	return f.tagObject(), err
}

func (f fakeTagGit) CatFileType(ctx context.Context, ref string) (string, error) {
	err := f.record(ctx, tagVerifyCall{method: "CatFileType", ref: ref})
	if f.tagType == "" {
		return "tag", err
	}

	return f.tagType, err
}

func (f fakeTagGit) RemoteTagObjectAtURL(ctx context.Context, repository, tag string, token runcontext.Credential) (string, error) {
	err := f.record(ctx, tagVerifyCall{method: "RemoteTagObjectAtURL", ref: tag, repository: repository, token: token})
	if f.remoteObject != "" {
		return f.remoteObject, err
	}

	return f.tagObject(), err
}

func (f fakeTagGit) RemoteTagCommit(ctx context.Context, repository, tag string, token runcontext.Credential) (string, error) {
	err := f.record(ctx, tagVerifyCall{method: "RemoteTagCommit", ref: tag, repository: repository, token: token})

	return f.tagCommit, err
}

func (f fakeTagGit) RemoteVersionTags(ctx context.Context, repository string, token runcontext.Credential) ([]string, error) {
	err := f.record(ctx, tagVerifyCall{method: "RemoteVersionTags", repository: repository, token: token})

	return f.tags, err
}

func (f fakeTagGit) record(ctx context.Context, call tagVerifyCall) error {
	if f.queries != nil {
		*f.queries++
	}

	if f.repositories != nil && call.repository != "" {
		*f.repositories = append(*f.repositories, call.repository)
	}

	if f.observe != nil {
		return f.observe(ctx, call)
	}

	return nil
}

func (f fakeTagGit) tagObject() string {
	if f.localObject != "" {
		return f.localObject
	}

	return "0123456789abcdef0123456789abcdef01234567"
}

const sha = "abcdef1234567890abcdef1234567890abcdef12"

func runVerify(g fakeTagGit, tag string) error {
	return apprelease.VerifyReleaseTag(context.Background(), g, io.Discard, apprelease.VerifyReleaseTagInput{
		ReleaseSHA: sha, Tag: tag, RepoURL: "https://codeberg.org/o/r.git",
	})
}

func TestVerifyReleaseTag_VerificationSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		failAt      int
		cause       error
		errContains string
	}{
		{name: "success"},
		{name: "head_failure", failAt: 1, cause: errVerifyTagHEAD, errContains: "resolve HEAD"},
		{name: "tag_type_failure", failAt: 2, cause: errVerifyTagType, errContains: "inspect local tag object"},
		{name: "local_object_failure", failAt: 3, cause: errVerifyTagLocalObject, errContains: "resolve local tag object"},
		{name: "remote_object_failure", failAt: 4, cause: errVerifyTagRemoteObject, errContains: "resolve remote tag object"},
		{name: "remote_commit_failure", failAt: 5, cause: errVerifyTagRemoteCommit, errContains: "resolve remote tag:"},
		{name: "remote_versions_failure", failAt: 6, cause: errVerifyTagRemoteVersions, errContains: "list remote tags"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			token := runcontext.ReleaseToken().Resolve(func(key string) string {
				switch key {
				case "FORGEJO_TOKEN":
					return "fixture-release-token"
				case "FORGEJO_SERVER_URL":
					return "https://forge.example"
				default:
					return ""
				}
			})
			require.True(t, token.Present(), "credential forwarding needs a nonempty control")

			const repository = "https://forge.example/team/release.git"

			wantCalls := []tagVerifyCall{
				{method: "RevParse", ref: "HEAD"},
				{method: "CatFileType", ref: "refs/tags/v1.2.3"},
				{method: "RevParse", ref: "refs/tags/v1.2.3"},
				{method: "RemoteTagObjectAtURL", repository: repository, ref: "v1.2.3", token: token},
				{method: "RemoteTagCommit", repository: repository, ref: "v1.2.3", token: token},
				{method: "RemoteVersionTags", repository: repository, token: token},
			}

			var calls []tagVerifyCall

			git := fakeTagGit{
				head: sha, tagCommit: sha, tags: []string{"v1.0.0", "v1.2.3", "v1.1.0"},
				localObject:  "0123456789abcdef0123456789abcdef01234567",
				remoteObject: "0123456789abcdef0123456789abcdef01234567",
				observe: func(gotCtx context.Context, call tagVerifyCall) error {
					require.Same(t, ctx, gotCtx, "%s must receive the caller's exact context", call.method)

					calls = append(calls, call)
					if len(calls) == testCase.failAt {
						return testCase.cause
					}

					return nil
				},
			}

			var out bytes.Buffer

			err := apprelease.VerifyReleaseTag(ctx, git, &out, apprelease.VerifyReleaseTagInput{
				ReleaseSHA: sha, Tag: "v1.2.3", RepoURL: repository, Token: token,
			})
			if testCase.cause != nil {
				require.ErrorIs(t, err, testCase.cause)
				require.ErrorContains(t, err, "verify-release-tag: "+testCase.errContains)
				require.Empty(t, out.String(), "failed verification must not announce success")

				wantCalls = wantCalls[:testCase.failAt]
			} else {
				require.NoError(t, err)
				require.Equal(t, "release tag v1.2.3 verified at abcdef1234567890abcdef1234567890abcdef12 (latest, points to release-sha)\n", out.String())
			}

			require.True(t, slices.Equal(calls, wantCalls), "calls = %v, want exact sequence and credentials %v", calls, wantCalls)
		})
	}
}

func TestVerifyReleaseTag_UsesOriginalRepositoryArgument(t *testing.T) {
	t.Parallel()

	for _, repository := range []string{"", "https://entry.example/o/r.git", "ssh://git@forge.example/o/r.git", "/local/repo.git"} {
		var queries []string

		git := fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3"}, repositories: &queries}

		err := apprelease.VerifyReleaseTag(t.Context(), git, io.Discard, apprelease.VerifyReleaseTagInput{ReleaseSHA: sha, Tag: "v1.2.3", RepoURL: repository})
		if err != nil {
			t.Fatal(err)
		}

		want := repository
		if want == "" {
			want = "origin"
		}

		if len(queries) != 3 {
			t.Fatalf("queries=%v, want object, commit and version queries", queries)
		}

		for _, got := range queries {
			if got != want {
				t.Fatalf("query argument=%q, want original argument %q, never an expanded URL", got, want)
			}
		}
	}
}

func TestVerifyReleaseTag_RejectsMalformedMatchingIDsBeforeQueries(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"short", strings.Repeat("g", 40), strings.Repeat("A", 40), strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("a", 63), strings.Repeat("a", 65), " " + strings.Repeat("a", 40)} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			calls := 0
			git := fakeTagGit{head: id, tagCommit: id, tags: []string{"v1.2.3"}, queries: &calls}

			err := apprelease.VerifyReleaseTag(t.Context(), git, io.Discard, apprelease.VerifyReleaseTagInput{ReleaseSHA: id, Tag: "v1.2.3", RepoURL: "https://example.invalid/repo.git"})
			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err=%v, want ErrValidation", err)
			}

			if calls != 0 {
				t.Errorf("Git queried %d times before input refusal", calls)
			}
		})
	}
}

func TestVerifyReleaseTag_AcceptsBothCommitIDLengths(t *testing.T) {
	t.Parallel()

	for _, size := range []int{40, 64} {
		id := strings.Repeat("a", size)

		git := fakeTagGit{head: id, tagCommit: id, tags: []string{"v1.2.3"}, localObject: strings.Repeat("b", size)}
		if err := apprelease.VerifyReleaseTag(t.Context(), git, io.Discard, apprelease.VerifyReleaseTagInput{ReleaseSHA: id, Tag: "v1.2.3"}); err != nil {
			t.Errorf("%d-character ID rejected: %v", size, err)
		}
	}
}

func TestVerifyReleaseTag_RejectsMalformedMatchingTagObjects(t *testing.T) {
	t.Parallel()

	git := fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3"}, localObject: "invalid", remoteObject: "invalid"}
	if err := runVerify(git, "v1.2.3"); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err=%v, want ErrValidation", err)
	}
}

func TestVerifyReleaseTag_Rejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		git         fakeTagGit
		tag         string
		wantCalls   int
		errContains string
	}{
		"checkout_mismatch":      {fakeTagGit{head: strings.Repeat("c", 40), tagCommit: sha, tags: []string{"v1.2.3"}}, "v1.2.3", 1, "does not match release-sha"},
		"lightweight_tag":        {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3"}, tagType: "commit"}, "v1.2.3", 2, "not an annotated tag object"},
		"remote_object_mismatch": {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3"}, remoteObject: strings.Repeat("d", 40)}, "v1.2.3", 4, "does not match remote object"},
		"unstable_tag":           {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3-rc1"}}, "v1.2.3-rc1", 0, "release tag must be stable"},
		"remote_tag_mismatch":    {fakeTagGit{head: sha, tagCommit: strings.Repeat("e", 40), tags: []string{"v1.2.3"}}, "v1.2.3", 5, "not release-sha"},
		"superseded":             {fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3", "v2.0.0"}}, "v1.2.3", 6, "has been superseded by v2.0.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			tc.git.queries = &calls

			var out bytes.Buffer

			err := apprelease.VerifyReleaseTag(t.Context(), tc.git, &out, apprelease.VerifyReleaseTagInput{ReleaseSHA: sha, Tag: tc.tag})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.ErrorContains(t, err, tc.errContains)
			require.Equal(t, tc.wantCalls, calls, "verification must stop at the refused boundary")
			require.Empty(t, out.String())
		})
	}
}

func TestVerifyReleaseTag_MissingInputs(t *testing.T) {
	t.Parallel()

	err := apprelease.VerifyReleaseTag(context.Background(), fakeTagGit{}, io.Discard, apprelease.VerifyReleaseTagInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing inputs should be a usage error, got %v", err)
	}
}

func TestVerifyReleaseTag_IgnoresNonSemverTags(t *testing.T) {
	t.Parallel()

	// "latest", "v1.2.3-rc1", "nightly" must not count as a newer release.
	g := fakeTagGit{head: sha, tagCommit: sha, tags: []string{"v1.2.3", "latest", "v1.2.3-rc1", "nightly"}}
	if err := runVerify(g, "v1.2.3"); err != nil {
		t.Errorf("non-semver tags should be ignored, got %v", err)
	}
}
