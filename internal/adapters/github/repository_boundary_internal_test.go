// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
)

func TestGitHubRepositoryBoundary_RefusesBeforeCredentialsOrRequests(t *testing.T) {
	t.Parallel()

	for _, repo := range []string{"owner/repo/extra", "owner/..", "./repo", "owner/repo?x=y", "owner/repo#frag", "owner/%2fescape", "owner/repo\\path", "owner/ repo", " owner/repo", "owner/repo\n", "owner//repo"} {
		t.Run(repo, func(t *testing.T) {
			reads, requests := 0, 0
			p := &Provider{Env: func(key string) string {
				if strings.Contains(key, "TOKEN") {
					reads++

					return "synthetic-canary"
				}

				if key == "GITHUB_SERVER_URL" {
					return defaultServerURL
				}

				if key == "GITHUB_REPOSITORY" {
					return repo
				}

				return ""
			}, HTTPClient: &http.Client{Transport: contractTransport(func(req *http.Request) (*http.Response, error) {
				requests++

				return contractResponse(req, 200, `{}`), nil
			})}}
			require.ErrorIs(t, p.CreateRelease(t.Context(), repo, provider.ReleaseSpec{Tag: "v1"}), errs.ErrUsage)
			_, err := p.FetchRepoMetadata(t.Context(), repo)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.ErrorIs(t, p.ValidateToken(t.Context(), "synthetic", repo), errs.ErrUsage)
			_, err = p.ValidateBotPermissions(t.Context(), repo)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.ErrorIs(t, p.UploadSARIF(t.Context(), provider.SARIFUpload{Repository: repo, Token: "synthetic", SARIF: []byte(`{}`)}), errs.ErrUsage)
			_, err = p.DownloadRunArtifact(t.Context(), provider.RunArtifactDownload{Repository: repo, RunID: "1", Name: "artifact", Dir: t.TempDir()})
			require.ErrorIs(t, err, errs.ErrUsage)
			_, err = p.ResolveForgeMavenRegistry()
			require.ErrorIs(t, err, errs.ErrUsage)
			_, err = p.ResolveForgeNPMRegistry()
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Zero(t, reads)
			require.Zero(t, requests)
		})
	}

	owner, repo, err := splitRepo("Some-Owner/repo.name_2")
	require.NoError(t, err)
	require.Equal(t, "Some-Owner", owner)
	require.Equal(t, "repo.name_2", repo)
}
