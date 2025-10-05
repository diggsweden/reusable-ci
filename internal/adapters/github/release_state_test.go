// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// callTrace renders each call as "METHOD path" for whole-trace comparison.
func callTrace(calls []recordedCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.Method+" "+call.Path)
	}

	return out
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))

	return decoded
}

func writeAssets(t *testing.T, contents map[string]string) map[string]string {
	t.Helper()

	dir := t.TempDir()
	paths := make(map[string]string, len(contents))

	for name, content := range contents {
		paths[name] = filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(paths[name], []byte(content), 0o600))
	}

	return paths
}

// TestCreateRelease_FinalStateAndOrderAreExact runs the draft-first flow with
// notes, prerelease and two assets, and compares the whole request trace, both
// release payloads and the release left behind, rather than the substrings
// and prefixes the flag and ordering tests check.
func TestCreateRelease_FinalStateAndOrderAreExact(t *testing.T) {
	t.Parallel()

	fake := newFake()

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	paths := writeAssets(t, map[string]string{"app.tgz": "app", "checksums.txt": "sums", "notes.md": "# Two\n"})

	err := providerForFake(srv).CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag: "v2.0.0", Name: "Two", NotesFile: paths["notes.md"], Prerelease: true,
		MakeLatest: provider.MakeLatestFalse, Assets: []string{paths["app.tgz"], paths["checksums.txt"]},
	})
	require.NoError(t, err)

	calls := fake.Calls()
	require.Equal(t, []string{
		"GET /repos/owner/repo/releases",
		"POST /repos/owner/repo/releases",
		"GET /repos/owner/repo/releases/1001/assets",
		"POST /repos/owner/repo/releases/1001/assets",
		"GET /repos/owner/repo/releases/1001/assets",
		"POST /repos/owner/repo/releases/1001/assets",
		"PATCH /repos/owner/repo/releases/1001",
	}, callTrace(calls))

	require.Equal(t, map[string]any{
		"tag_name": "v2.0.0", "name": "Two", "draft": true, "prerelease": true, "make_latest": "false", "body": "# Two\n",
	}, decodeBody(t, calls[1].Body))
	require.Equal(t, map[string]any{
		"tag_name": "v2.0.0", "name": "Two", "draft": false, "prerelease": true, "make_latest": "false", "body": "# Two\n",
	}, decodeBody(t, calls[6].Body))

	require.Equal(t, &recordedRelease{
		ID: 1001, TagName: "v2.0.0", Name: "Two", Prerelease: true, MakeLatest: "false", Body: "# Two\n",
		Assets: []*recordedAsset{{ID: 1002, Name: "app.tgz", Body: "app"}, {ID: 1003, Name: "checksums.txt", Body: "sums"}},
	}, fake.releaseSnapshot("v2.0.0"))
}

// TestPublishRelease_UpdatesAnExistingDraftInPlace is the rerun of a draft
// publication. GitHub's tag endpoint omits drafts, so a lookup through it
// found nothing and created a second release on the same tag. The existing
// draft must be the one updated: no release is created, the assets are
// reconciled first and the metadata is written last.
func TestPublishRelease_UpdatesAnExistingDraftInPlace(t *testing.T) {
	t.Parallel()

	fake := newFake()
	draft := fake.preloadRelease(true, false)
	draft.Name, draft.Body = "old", "old notes"
	draft.Assets = []*recordedAsset{{ID: 555, Name: "stale.tgz", Body: "stale"}}

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	paths := writeAssets(t, map[string]string{"app.tgz": "app", "notes.md": "new notes"})

	err := providerForFake(srv).PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag: "v1.0.0", Name: "renamed", NotesFile: paths["notes.md"], Draft: true, Assets: []string{paths["app.tgz"]},
	})
	require.NoError(t, err)

	calls := fake.Calls()
	require.Equal(t, []string{
		"GET /repos/owner/repo/releases",
		"GET /repos/owner/repo/releases/1001/assets",
		"POST /repos/owner/repo/releases/1001/assets",
		"GET /repos/owner/repo/releases/1001/assets",
		"DELETE /repos/owner/repo/releases/assets/555",
		"PATCH /repos/owner/repo/releases/1001",
	}, callTrace(calls))

	require.Equal(t, map[string]any{
		"tag_name": "v1.0.0", "name": "renamed", "draft": true, "prerelease": false, "make_latest": "true", "body": "new notes",
	}, decodeBody(t, calls[5].Body))

	require.Equal(t, &recordedRelease{
		ID: 1001, TagName: "v1.0.0", Name: "renamed", Draft: true, MakeLatest: "true", Body: "new notes",
		Assets: []*recordedAsset{{ID: 1002, Name: "app.tgz", Body: "app"}},
	}, fake.releaseSnapshot("v1.0.0"))
}

// TestPublishRelease_FailedUploadLeavesTheExistingReleaseAsItWas: a draft
// being published whose asset upload fails must stay a draft with its old
// name, notes and assets, and nothing stale is deleted. Writing the metadata
// first made it visible without the artifact it announces.
func TestPublishRelease_FailedUploadLeavesTheExistingReleaseAsItWas(t *testing.T) {
	t.Parallel()

	fake := newFake()
	draft := fake.preloadRelease(true, false)
	draft.Name, draft.Body = "old", "old notes"
	draft.Assets = []*recordedAsset{{ID: 555, Name: "stale.tgz", Body: "stale"}}
	fake.failUploadName = "app.tgz"
	fake.failUploadRemaining = 1

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	paths := writeAssets(t, map[string]string{"app.tgz": "app", "notes.md": "new notes"})

	err := providerForFake(srv).PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag: "v1.0.0", Name: "renamed", NotesFile: paths["notes.md"], Assets: []string{paths["app.tgz"]},
	})
	require.ErrorContains(t, err, `upload asset "app.tgz"`)

	require.Equal(t, []string{
		"GET /repos/owner/repo/releases",
		"GET /repos/owner/repo/releases/1001/assets",
		"POST /repos/owner/repo/releases/1001/assets",
	}, callTrace(fake.Calls()))

	require.Equal(t, &recordedRelease{
		ID: 1001, TagName: "v1.0.0", Name: "old", Draft: true, Body: "old notes",
		Assets: []*recordedAsset{{ID: 555, Name: "stale.tgz", Body: "stale"}},
	}, fake.releaseSnapshot("v1.0.0"))
}

// TestCreateRelease_StableMatchEdges pins the two ways an existing stable
// release can look like the requested one and not be it. Both are refused
// rather than reported as an idempotent rerun, and nothing is deleted or
// uploaded: the release with an extra asset carries something this run did
// not produce, and an asset of the same size with different bytes is the
// case a size comparison alone would accept.
func TestCreateRelease_StableMatchEdges(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		existing  []*recordedAsset
		requested map[string]string
		want      string
	}{
		"more assets than requested": {
			existing:  []*recordedAsset{{ID: 2001, Name: "app.tgz", Body: "artifact"}, {ID: 2002, Name: "extra.tgz", Body: "extra"}},
			requested: map[string]string{"app.tgz": "artifact"},
			want:      "has 2 assets, requested release has 1",
		},
		"equal size different digest": {
			existing:  []*recordedAsset{{ID: 2001, Name: "app.tgz", Body: "artifact"}},
			requested: map[string]string{"app.tgz": "artiface"},
			want:      `asset "app.tgz" does not match requested size and digest`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := newFake()
			release := fake.preloadRelease(false, false)
			release.Name, release.Body = "v1.0.0", "notes\n"
			release.Assets = testCase.existing

			srv := httptest.NewServer(combinedHandler(fake))
			defer srv.Close()

			paths := writeAssets(t, testCase.requested)
			paths["notes.md"] = writeAssets(t, map[string]string{"notes.md": "notes\n"})["notes.md"]

			assets := make([]string, 0, len(testCase.requested))
			for name := range testCase.requested {
				assets = append(assets, paths[name])
			}

			err := providerForFake(srv).CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
				Tag: "v1.0.0", Name: "v1.0.0", NotesFile: paths["notes.md"], Assets: assets,
			})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.ErrorContains(t, err, testCase.want)

			for _, call := range fake.Calls() {
				require.Equal(t, "GET", call.Method, "a refused stable rerun mutated the release: %+v", call)
			}

			require.Equal(t, testCase.existing, fake.releaseSnapshot("v1.0.0").Assets)
		})
	}
}

// TestCreateRelease_APublishThatNeverAppliedIsNotRecovered is the other
// half of the post-failure recovery. When the publishing PATCH fails after
// the server applied it, the follow-up GET finds the release published and
// the run succeeds. When the PATCH never applied, the release is still the
// draft, and the same GET must say so: the draft flag is part of the
// metadata comparison, so a comparison of name and body alone would call a
// draft nobody can see a published release.
func TestCreateRelease_APublishThatNeverAppliedIsNotRecovered(t *testing.T) {
	t.Parallel()

	fake := newFake()
	fake.failPatchRemaining = 1

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	err := providerForFake(srv).CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag: "v1.0.0", Name: "v1.0.0", MakeLatest: provider.MakeLatestTrue,
	})
	require.ErrorContains(t, err, `publish release "v1.0.0"`)

	require.Equal(t, []string{
		"GET /repos/owner/repo/releases",
		"POST /repos/owner/repo/releases",
		"PATCH /repos/owner/repo/releases/1001",
		"GET /repos/owner/repo/releases/1001",
	}, callTrace(fake.Calls()))

	final := fake.releaseSnapshot("v1.0.0")
	require.NotNil(t, final)
	require.True(t, final.Draft, "the release was reported published while still a draft")
}
