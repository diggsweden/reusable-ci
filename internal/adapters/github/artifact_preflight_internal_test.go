// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"archive/zip"
	"bytes"
	"context"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubPreflight_DuplicateAssetsBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	files := []string{filepath.Join(root, "one", "asset"), filepath.Join(root, "two", "asset")}
	for _, file := range files {
		require.NoError(t, os.Mkdir(filepath.Dir(file), 0o700))
		require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))
	}

	calls := 0

	p := &Provider{APIBaseOverride: "https://github.invalid", Env: func(string) string { return "" }, HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
		calls++

		return contractResponse(req, 200, `{"id":42,"draft":true,"prerelease":true}`), nil
	})}}
	for _, invoke := range []func(context.Context, string, provider.ReleaseSpec) error{p.CreateRelease, p.PublishRelease} {
		err := invoke(t.Context(), "o/r", provider.ReleaseSpec{Tag: "v1", Assets: files})
		assert.ErrorIs(t, err, errs.ErrValidation) //nolint:testifylint // keep the independent no-request observation running.
		assert.Zero(t, calls)
	}
}

func TestGitHubPreflight_PatternsAndAllMatchedDestinations(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"syntax", "later-name", "later-path", "empty", "success"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "canary"), []byte("old"), 0o600))

			body := `{"artifacts":[]}`

			switch kind {
			case "later-name":
				body = `{"artifacts":[{"id":1,"name":"good"},{"id":2,"name":"bad\nname"}]}`
			case "later-path":
				body = `{"artifacts":[{"id":1,"name":"good"},{"id":2,"name":".."}]}`
			case "success":
				body = `{"artifacts":[{"id":1,"name":"good"}]}`
			}

			var archive bytes.Buffer

			writer := zip.NewWriter(&archive)
			file, err := writer.Create("value.txt")
			require.NoError(t, err)
			_, err = file.Write([]byte("fresh"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			calls := 0
			credentials := 0
			p := &Provider{APIBaseOverride: "https://github.invalid", Env: func(key string) string {
				if key == "GH_TOKEN" || key == "GITHUB_TOKEN" {
					credentials++
				}

				return ""
			}, HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
				calls++

				if req.URL.Host == "blob.invalid" {
					return contractResponse(req, 200, archive.String()), nil
				}

				if strings.HasSuffix(req.URL.Path, "/1/zip") {
					response := contractResponse(req, 302, "")
					response.Header.Set("Location", "https://blob.invalid/first.zip")

					return response, nil
				}

				return contractResponse(req, 200, body), nil
			})}}

			pattern := "*"
			if kind == "syntax" {
				pattern = "["
			}

			_, err = p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Repository: "o/r", RunID: "1", Pattern: pattern, Dir: root})

			switch kind {
			case "empty":
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			case "syntax":
				require.ErrorIs(t, err, errs.ErrUsage)
				require.Zero(t, calls)
				require.Zero(t, credentials)
			case "success":
				require.NoError(t, err)
				require.Equal(t, 3, calls)

				data, readErr := os.ReadFile(filepath.Join(root, "good", "value.txt"))
				require.NoError(t, readErr)
				require.Equal(t, "fresh", string(data))
			default:
				require.ErrorIs(t, err, errs.ErrValidation)
				assert.Equal(t, 1, calls)
			}

			entries, err := os.ReadDir(root)
			require.NoError(t, err)

			want := 1
			if kind == "success" {
				want = 2
			}

			require.Len(t, entries, want)
		})
	}
}

func TestGitHubArtifactStatus_ClassifiesListAndDownloadURL(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"", "*"} {
		for _, stage := range []string{"list", "url"} {
			for _, tc := range []struct {
				status int
				want   error
			}{{401, errs.ErrPermissionDenied}, {403, errs.ErrPermissionDenied}, {404, errs.ErrMissingInput}, {429, errs.ErrRateLimited}, {500, errs.ErrDependencyUnavailable}} {
				calls := 0
				p := &Provider{APIBaseOverride: "https://github.invalid", Env: func(string) string { return "" }, HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
					calls++

					if stage == "url" && strings.HasSuffix(req.URL.Path, "/artifacts") {
						return contractResponse(req, 200, `{"artifacts":[{"id":7,"name":"wanted"}]}`), nil
					}

					return contractResponse(req, tc.status, `{"message":"fixture"}`), nil
				})}}
				_, err := p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Repository: "o/r", RunID: "1", Name: "wanted", Pattern: pattern, Dir: t.TempDir()})
				require.ErrorIs(t, err, tc.want)

				wantCalls := 1
				if stage == "url" {
					wantCalls = 2
				}

				require.Equal(t, wantCalls, calls)
			}
		}
	}
}
