// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
)

func TestArtifactRepository_RejectsForeignBeforeRequests(t *testing.T) {
	t.Parallel()

	for _, repo := range []string{"other/repo", "owner/repo", ""} {
		calls := 0
		p := &forgejo.Provider{Env: envMap(map[string]string{"FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_RUN_ID": "1", "ACTIONS_RUNTIME_URL": "https://runtime.invalid/", "ACTIONS_RUNTIME_TOKEN": "synthetic"}), HTTPClient: &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
			calls++

			return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("refused")), Request: req}, nil
		})}}

		_, err := p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Name: "artifact", Repository: repo, Dir: t.TempDir()})
		if repo == "other/repo" {
			require.ErrorIs(t, err, errs.ErrUnsupported)
			require.Zero(t, calls)
		} else {
			require.ErrorIs(t, err, errs.ErrPermissionDenied)
			require.Equal(t, 1, calls)
		}
	}
}

func TestReleaseLocalPreflight_StopsBeforeForgejoRequests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	good := filepath.Join(root, "good")
	require.NoError(t, os.WriteFile(good, []byte("asset"), 0o600))

	calls := 0
	p := &forgejo.Provider{APIBaseOverride: "https://forgejo.invalid", Env: envMap(nil), HTTPClient: &http.Client{Transport: releaseAssetTransport(func(*http.Request) (*http.Response, error) {
		calls++

		return nil, errs.ErrDependencyUnavailable
	})}}
	spec := provider.ReleaseSpec{Tag: "v1.2.3", Assets: []string{good, filepath.Join(root, "missing")}}
	require.ErrorIs(t, p.CreateRelease(t.Context(), "owner/repo", spec), os.ErrNotExist)
	require.ErrorIs(t, p.PublishRelease(t.Context(), "owner/repo", spec), os.ErrNotExist)
	require.Zero(t, calls)
}
