// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitlabserver"
)

// TestValidateBotPermissions_RefusalsAreMissingAndOutagesFail answers each
// probe with its own status. A refusal or a hidden project turns only that
// permission off; an unavailable or rate-limited GitLab fails the check with
// its class, because `validate auth bot-permissions` would otherwise report an
// outage as an invalid token (exit 77) and send the operator to widen a scope.
func TestValidateBotPermissions_RefusalsAreMissingAndOutagesFail(t *testing.T) {
	t.Parallel()

	const project = "/api/v4/projects/group%2Fproject"

	for _, tc := range []struct {
		name                 string
		user, repo, branches int
		want                 provider.BotPermissions
		wantErr              error
	}{
		{name: "user denied", user: http.StatusUnauthorized, repo: http.StatusOK, branches: http.StatusOK, want: provider.BotPermissions{RepoAccessible: true, BranchesAccessible: true}},
		{name: "project hidden", user: http.StatusOK, repo: http.StatusNotFound, branches: http.StatusOK, want: provider.BotPermissions{UserAccessible: true, BranchesAccessible: true}},
		{name: "branches forbidden", user: http.StatusOK, repo: http.StatusOK, branches: http.StatusForbidden, want: provider.BotPermissions{UserAccessible: true, RepoAccessible: true}},
		{name: "project unavailable", user: http.StatusOK, repo: http.StatusBadGateway, branches: http.StatusOK, wantErr: errs.ErrDependencyUnavailable},
		{name: "user rate limited", user: http.StatusTooManyRequests, repo: http.StatusOK, branches: http.StatusOK, wantErr: errs.ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := fakegitlabserver.New(t)
			for path, status := range map[string]int{"/api/v4/user": tc.user, project: tc.repo, project + "/repository/branches": tc.branches} {
				srv.OnGet(path, func(fakegitlabserver.Request) fakegitlabserver.Response {
					return fakegitlabserver.Response{Status: status, Body: `{}`}
				})
			}

			p := &gitlab.Provider{Env: envFunc(map[string]string{"GITLAB_TOKEN": "glpat_test"}), APIBaseOverride: srv.URL(), HTTPClient: srv.Client()}

			got, err := p.ValidateBotPermissions(context.Background(), "group/project")
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
