// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
)

// renderCall is one renderer invocation as the use case made it.
type renderCall struct {
	kind, backend, config, tag, output, repositoryURL string
}

// recordingChangelogRenderer records every call and fails the kind named in
// failOn after recording it. RenderFull writes a changelog so its line count
// can be reported.
type recordingChangelogRenderer struct {
	calls  []renderCall
	failOn string
}

var errRendererRefused = errors.New("renderer refused")

func (r *recordingChangelogRenderer) RenderFull(_ context.Context, backend, config, tag, outputPath, repositoryURL string) error {
	r.calls = append(r.calls, renderCall{"full", backend, config, tag, outputPath, repositoryURL})
	if r.failOn == "full" {
		return errRendererRefused
	}

	return os.WriteFile(outputPath, []byte("# Changelog\n\n## v1.2.3\n"), 0o600)
}

func (r *recordingChangelogRenderer) RenderBody(_ context.Context, backend, config, tag, repositoryURL string) (string, error) {
	r.calls = append(r.calls, renderCall{"body", backend, config, tag, "", repositoryURL})
	if r.failOn == "body" {
		return "", errRendererRefused
	}

	return "- feat: a change\n", nil
}

const renderRepositoryURL = "https://forge.example.invalid/owner/repo"

// TestChangelogRender_EachBackendAndPathWritesExactFiles covers both backends
// with default and custom paths. Each renderer call is compared whole, so the
// changelog config cannot be swapped with the body config or the output path
// with another; the commit body and message are compared byte for byte.
//
// Not parallel: the default-path rows change the working directory.
func TestChangelogRender_EachBackendAndPathWritesExactFiles(t *testing.T) {
	const message = "chore(release): bump to v1.2.3\n\n- feat: a change\n\n[skip ci]\n\nRelease-Request: v1.2.3\n"

	for _, backend := range []string{"", "git-cliff"} {
		for _, custom := range []bool{false, true} {
			name := backend + " default paths"
			if custom {
				name = backend + " custom paths"
			}

			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)

				in := appversion.ChangelogRenderInput{
					Backend: backend, Tag: "v1.2.3", RepositoryURL: renderRepositoryURL,
					ChangelogConfig: ".chglog/full.yml", CommitBodyConfig: ".chglog/body.yml",
					CommitTrailers: "Release-Request: v1.2.3",
				}

				changelog, body, msg := "CHANGELOG.md", "commit-body.txt", "commit-msg.txt"

				if custom {
					require.NoError(t, os.Mkdir(filepath.Join(dir, "out"), 0o700))

					changelog, body, msg = filepath.Join(dir, "out", "CHANGES.md"), filepath.Join(dir, "out", "body.txt"), filepath.Join(dir, "out", "msg.txt")
					in.ChangelogPath, in.CommitBodyPath, in.CommitMessagePath = changelog, body, msg
				}

				renderer := &recordingChangelogRenderer{}

				var out bytes.Buffer

				res, err := appversion.ChangelogRender(t.Context(), &fakeChangelogRenderGit{status: " M " + changelog}, renderer, &out, in)
				require.NoError(t, err)
				require.Equal(t, &appversion.ChangelogRenderOutput{Rendered: true}, res)

				wantBackend := backend
				if wantBackend == "" {
					wantBackend = "git-chglog"
				}

				require.Equal(t, []renderCall{
					{"full", wantBackend, ".chglog/full.yml", "v1.2.3", changelog, renderRepositoryURL},
					{"body", wantBackend, ".chglog/body.yml", "v1.2.3", "", renderRepositoryURL},
				}, renderer.calls)
				require.Equal(t, "- feat: a change\n", readTestFile(t, body))
				require.Equal(t, message, readTestFile(t, msg))
				require.Equal(t, "Generated "+changelog+" (3 lines)\n", out.String())
			})
		}
	}
}

// TestChangelogRender_UnchangedOrFailedRenderWritesNoCommitFiles: an
// unchanged changelog needs no release commit, so the body is never rendered
// and no commit files appear; a failed changelog render stops before the body;
// a failed body render leaves neither file. The signing step treats a present
// commit message as the go-ahead, so none of these may produce one.
//
// Not parallel: changes the working directory.
func TestChangelogRender_UnchangedOrFailedRenderWritesNoCommitFiles(t *testing.T) {
	tests := []struct {
		name   string
		status string
		failOn string
		calls  []string
	}{
		{name: "unchanged", status: "", calls: []string{"full"}},
		{name: "changelog render fails", status: " M CHANGELOG.md", failOn: "full", calls: []string{"full"}},
		{name: "body render fails", status: " M CHANGELOG.md", failOn: "body", calls: []string{"full", "body"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			renderer := &recordingChangelogRenderer{failOn: tc.failOn}
			_, err := appversion.ChangelogRender(t.Context(), &fakeChangelogRenderGit{status: tc.status}, renderer, &bytes.Buffer{}, appversion.ChangelogRenderInput{
				Tag: "v1.2.3", RepositoryURL: renderRepositoryURL, ChangelogConfig: "full", CommitBodyConfig: "body",
			})

			if tc.failOn == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errRendererRefused)
			}

			kinds := make([]string, 0, len(renderer.calls))
			for _, call := range renderer.calls {
				kinds = append(kinds, call.kind)
			}

			require.Equal(t, tc.calls, kinds)

			for _, name := range []string{"commit-body.txt", "commit-msg.txt"} {
				_, statErr := os.Lstat(name)
				require.ErrorIs(t, statErr, fs.ErrNotExist, name)
			}
		})
	}
}

// TestChangelogRender_ReplacesASymlinkedCommitMessageInsteadOfFollowingIt: a
// checkout can carry commit-msg.txt as a symlink. The message must land in a
// regular file at that path and the link's target must keep its bytes.
//
// Not parallel: changes the working directory.
func TestChangelogRender_ReplacesASymlinkedCommitMessageInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	outside := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("untouched\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "commit-msg.txt")))

	_, err := appversion.ChangelogRender(t.Context(), &fakeChangelogRenderGit{status: " M CHANGELOG.md"}, &recordingChangelogRenderer{}, &bytes.Buffer{}, appversion.ChangelogRenderInput{
		Tag: "v1.2.3", RepositoryURL: renderRepositoryURL, ChangelogConfig: "full", CommitBodyConfig: "body",
	})
	require.NoError(t, err)

	info, err := os.Lstat(filepath.Join(dir, "commit-msg.txt"))
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "commit-msg.txt is still a symlink")
	require.Equal(t, "untouched\n", readTestFile(t, outside))
}
