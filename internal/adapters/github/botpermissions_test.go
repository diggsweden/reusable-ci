// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitserver"
)

// TestValidateBotPermissions_RefusalsAreMissingAndOutagesFail answers each
// probe with its own status. A refusal or a hidden repository (GitHub answers
// 404 for a private repository the token cannot see) turns only that
// permission off; an unavailable or rate-limited API fails the check with its
// class instead of reading as an invalid token.
func TestValidateBotPermissions_RefusalsAreMissingAndOutagesFail(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		user, repo, branches int
		want                 provider.BotPermissions
		wantErr              error
	}{
		{name: "user denied", user: http.StatusUnauthorized, repo: http.StatusOK, branches: http.StatusOK, want: provider.BotPermissions{RepoAccessible: true, BranchesAccessible: true}},
		{name: "repository hidden", user: http.StatusOK, repo: http.StatusNotFound, branches: http.StatusOK, want: provider.BotPermissions{UserAccessible: true, BranchesAccessible: true}},
		{name: "branches forbidden", user: http.StatusOK, repo: http.StatusOK, branches: http.StatusForbidden, want: provider.BotPermissions{UserAccessible: true, RepoAccessible: true}},
		{name: "branches unavailable", user: http.StatusOK, repo: http.StatusOK, branches: http.StatusServiceUnavailable, wantErr: errs.ErrDependencyUnavailable},
		{name: "repository rate limited", user: http.StatusOK, repo: http.StatusTooManyRequests, branches: http.StatusOK, wantErr: errs.ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := fakegitserver.New(t)
			for path, status := range map[string]int{"/user": tc.user, "/repos/owner/repo": tc.repo, "/repos/owner/repo/branches": tc.branches} {
				srv.OnGet(path, func(fakegitserver.Request) fakegitserver.Response {
					return fakegitserver.Response{Status: status, Body: `{}`}
				})
			}

			p := &github.Provider{Env: envFunc(map[string]string{"GITHUB_TOKEN": "ghs_AAA"}), APIBaseOverride: srv.URL(), HTTPClient: srv.Client()}

			got, err := p.ValidateBotPermissions(context.Background(), "owner/repo")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || errors.Is(err, errs.ErrPermissionDenied) || got != nil {
					t.Fatalf("got %+v, %v; want only %v", got, err, tc.wantErr)
				}

				return
			}

			if err != nil || got == nil || *got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}
