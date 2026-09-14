// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestCredentialAuthority_EveryRoleSendsOnlyATokenIssuedForItsServer runs
// every token-bearing role of the adapter in three complete runner and target
// contexts. A GitHub job token must not reach a Forgejo host; a Forgejo
// runner's job token reaches its own server and is used there by every role;
// and the same job token must not follow a target moved to another server.
// The transport refuses every request, so each role's first request is the
// one inspected, and every header of every request is searched for the job
// token, not only Authorization.
func TestCredentialAuthority_EveryRoleSendsOnlyATokenIssuedForItsServer(t *testing.T) {
	t.Parallel()

	const jobToken = "synthetic-job-token-0123456789" //nolint:gosec // G101: synthetic fixture whose absence is asserted.

	tests := []struct {
		name     string
		env      map[string]string
		host     string
		wantSent bool
	}{
		{
			name: "github runner job token, third-party forgejo",
			env: map[string]string{
				"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_TOKEN": jobToken,
				"FORGEJO_SERVER_URL": "https://forgejo.invalid",
			},
			host: "forgejo.invalid",
		},
		{
			name: "forgejo runner job token, its own server",
			env: map[string]string{
				"GITHUB_ACTIONS": "true", "FORGEJO_ACTIONS": "true", "GITHUB_TOKEN": jobToken,
				"FORGEJO_SERVER_URL": "https://forgejo.invalid",
			},
			host:     "forgejo.invalid",
			wantSent: true,
		},
		{
			name: "forgejo runner job token, target moved to another server",
			env: map[string]string{
				"GITHUB_ACTIONS": "true", "FORGEJO_ACTIONS": "true", "FORGEJO_TOKEN": jobToken,
				"FORGEJO_SERVER_URL": "https://forgejo.invalid", "CI_SERVER_URL": "https://other.invalid",
			},
			host: "other.invalid",
		},
	}

	file := filepath.Join(t.TempDir(), "app.tgz")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))

	roles := map[string]func(context.Context, *forgejo.Provider) error{
		"bot permissions": func(ctx context.Context, p *forgejo.Provider) error {
			_, err := p.ValidateBotPermissions(ctx, "owner/repo")

			return err
		},
		"repo metadata": func(ctx context.Context, p *forgejo.Provider) error {
			_, err := p.FetchRepoMetadata(ctx, "owner/repo")

			return err
		},
		"delete tag": func(ctx context.Context, p *forgejo.Provider) error {
			return p.DeleteTag(ctx, "forgejo.invalid/owner/img:old")
		},
		"container versions": func(ctx context.Context, p *forgejo.Provider) error {
			_, err := p.ListContainerPackageVersions(ctx, "owner", "img")

			return err
		},
		"upload release asset": func(ctx context.Context, p *forgejo.Provider) error {
			return p.UploadReleaseAsset(ctx, "v1", file)
		},
		"publish release": func(ctx context.Context, p *forgejo.Provider) error {
			return p.PublishRelease(ctx, "owner/repo", provider.ReleaseSpec{Tag: "v1"})
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{"FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_ACTOR": "bot"}
			for key, value := range tc.env {
				env[key] = value
			}

			for role, run := range roles {
				var (
					mu       sync.Mutex
					requests []*http.Request
				)

				p := &forgejo.Provider{Env: envMap(env), HTTPClient: &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
					mu.Lock()

					requests = append(requests, req)

					mu.Unlock()

					return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"refused"}`)), Request: req}, nil
				})}}

				_ = run(t.Context(), p)

				require.NotEmpty(t, requests, "%s sent nothing, so its credential was never exercised", role)

				for _, req := range requests {
					require.Equal(t, tc.host, req.URL.Host, role)

					if tc.wantSent {
						require.Equal(t, "token "+jobToken, req.Header.Get("Authorization"), role)

						continue
					}

					for name, values := range req.Header {
						require.NotContains(t, strings.Join(values, " "), jobToken, "%s header %s", role, name)
					}
				}
			}

			p := &forgejo.Provider{Env: envMap(env)}
			maven, err := p.ResolveForgeMavenRegistry()
			require.NoError(t, err)

			npm, err := p.ResolveForgeNPMRegistry()
			require.NoError(t, err)

			auth, err := p.ResolveRegistryAuth()

			if tc.wantSent {
				require.NoError(t, err)
				require.Equal(t, []string{jobToken, jobToken, jobToken}, []string{maven.Token, npm.Token, auth.Token})

				return
			}

			require.Empty(t, maven.Token+npm.Token)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.NotContains(t, err.Error(), jobToken)
			require.True(t, strings.HasPrefix(maven.URL, "https://"+tc.host+"/"), maven.URL)
		})
	}
}
