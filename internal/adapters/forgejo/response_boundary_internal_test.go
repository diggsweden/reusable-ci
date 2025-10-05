// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type responseBoundaryTransport func(*http.Request) (*http.Response, error)

func (f responseBoundaryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestForgejoResponseBoundary_RejectsMalformedJSONAndIDs(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"{", "null", "{}", `{"id":0}`, `{"id":-1}`} {
		root := t.TempDir()
		file := filepath.Join(root, "asset")
		require.NoError(t, os.WriteFile(file, []byte("owned"), 0o600))

		calls := 0
		p := &Provider{APIBaseOverride: "https://forgejo.invalid", Env: func(key string) string {
			if key == "FORGEJO_REPOSITORY" {
				return "o/r"
			}

			return ""
		}, HTTPClient: &http.Client{Transport: responseBoundaryTransport(func(req *http.Request) (*http.Response, error) {
			calls++

			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}}

		if body != "{" {
			var err error

			require.NotPanics(t, func() { err = p.UploadReleaseAsset(t.Context(), "v1", file) })
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Equal(t, 1, calls)

			spec := provider.ReleaseSpec{Tag: "v1", Assets: []string{file}}
			require.ErrorIs(t, p.CreateRelease(t.Context(), "o/r", spec), errs.ErrMalformedInput)
			require.ErrorIs(t, p.PublishRelease(t.Context(), "o/r", spec), errs.ErrMalformedInput)
			require.Equal(t, 3, calls)
		} else {
			var output map[string]any

			err := p.getRuntimeJSON(t.Context(), "https://runtime.invalid/list", "synthetic", &output)
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.EqualValues(t, 65, errs.ExitCodeFromError(err))
			_, err = p.createContainer(t.Context(), runtimeUploadCreds{url: "https://runtime.invalid", token: "synthetic", runID: "1"}, "artifact", 1)
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.EqualValues(t, 65, errs.ExitCodeFromError(err))
			require.Equal(t, 2, calls)
		}
	}
}

func TestForgejoPreflight_MissingFilesAndMalformedPatterns(t *testing.T) {
	t.Parallel()

	calls := 0
	env := map[string]string{"FORGEJO_REPOSITORY": "o/r", "FORGEJO_RUN_ID": "1", "ACTIONS_RUNTIME_URL": "https://runtime.invalid", "ACTIONS_RUNTIME_TOKEN": "synthetic"}
	p := &Provider{APIBaseOverride: "https://forgejo.invalid", Env: func(key string) string { return env[key] }, HTTPClient: &http.Client{Transport: responseBoundaryTransport(func(req *http.Request) (*http.Response, error) {
		calls++

		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"value":[]}`)), Request: req}, nil
	})}}
	root := t.TempDir()
	_, err := p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Pattern: "[", Dir: root})
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Zero(t, calls)

	file := filepath.Join(root, "missing", "asset")

	spec := provider.ReleaseSpec{Tag: "v1", Assets: []string{file}}
	for _, err := range []error{p.UploadReleaseAsset(t.Context(), "v1", file), p.CreateRelease(t.Context(), "o/r", spec), p.PublishRelease(t.Context(), "o/r", spec)} {
		require.ErrorIs(t, err, errs.ErrMissingInput)
		require.EqualValues(t, 66, errs.ExitCodeFromError(err))
	}

	require.Zero(t, calls)
	_, err = p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Pattern: "*", Dir: root})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}
