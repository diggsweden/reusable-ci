// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// permissionServer answers the three probe endpoints in memory with the supplied
// status codes, so each permission can be denied independently.
func permissionServer(user, repo, branches int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The branches endpoint decodes into a slice, the other two into
		// objects; a body of the wrong shape fails the probe for the
		// wrong reason.
		status, body := http.StatusNotFound, "{}"

		switch {
		case strings.HasSuffix(r.URL.Path, "/branches"):
			status, body = branches, "[]"
		case strings.Contains(r.URL.Path, "/repos/"):
			status = repo
		case strings.HasSuffix(r.URL.Path, "/user"):
			status = user
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		_, _ = w.Write([]byte(body))
	})
}

// TestValidateBotPermissions_ReportsEachProbeIndependently covers a
// function with no test at all.
//
// The three flags exist so an operator can see *which* permission the bot
// token is missing. If one probe's outcome leaked into another the
// diagnostic would point at the wrong scope, which is worse than no
// diagnostic — someone would widen the wrong permission.
func TestValidateBotPermissions_ReportsEachProbeIndependently(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                             string
		user, repo, branches             int
		wantUser, wantRepo, wantBranches bool
		wantErr                          error
	}{
		{
			name: "all granted",
			user: http.StatusOK, repo: http.StatusOK, branches: http.StatusOK,
			wantUser: true, wantRepo: true, wantBranches: true,
		},
		{
			// A token scoped to the repo but not to the user endpoint.
			name: "user denied",
			user: http.StatusForbidden, repo: http.StatusOK, branches: http.StatusOK,
			wantUser: false, wantRepo: true, wantBranches: true,
		},
		{
			name: "repo denied",
			user: http.StatusOK, repo: http.StatusNotFound, branches: http.StatusOK,
			wantUser: true, wantRepo: false, wantBranches: true,
		},
		{
			// The one that matters for pushing a release branch.
			name: "branches denied",
			user: http.StatusOK, repo: http.StatusOK, branches: http.StatusForbidden,
			wantUser: true, wantRepo: true, wantBranches: false,
		},
		{
			name: "all denied",
			user: http.StatusUnauthorized, repo: http.StatusUnauthorized, branches: http.StatusUnauthorized,
			wantUser: false, wantRepo: false, wantBranches: false,
		},
		{
			// An outage is not a missing permission: reporting it as one
			// would send the operator to widen a scope.
			name: "repo unavailable",
			user: http.StatusOK, repo: http.StatusServiceUnavailable, branches: http.StatusOK,
			wantErr: errs.ErrDependencyUnavailable,
		},
		{
			name: "branches rate limited",
			user: http.StatusOK, repo: http.StatusOK, branches: http.StatusTooManyRequests,
			wantErr: errs.ErrRateLimited,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := permissionServer(tc.user, tc.repo, tc.branches)

			p := &forgejo.Provider{
				Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
				HTTPClient:      inMemoryClient(handler),
				APIBaseOverride: "https://forgejo.invalid",
			}

			got, err := p.ValidateBotPermissions(context.Background(), "owner/repo")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || errors.Is(err, errs.ErrPermissionDenied) || got != nil {
					t.Fatalf("got %+v, %v; want only %v", got, err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.UserAccessible != tc.wantUser {
				t.Errorf("UserAccessible = %v, want %v", got.UserAccessible, tc.wantUser)
			}

			if got.RepoAccessible != tc.wantRepo {
				t.Errorf("RepoAccessible = %v, want %v", got.RepoAccessible, tc.wantRepo)
			}

			if got.BranchesAccessible != tc.wantBranches {
				t.Errorf("BranchesAccessible = %v, want %v", got.BranchesAccessible, tc.wantBranches)
			}
		})
	}
}

// TestValidateBotPermissions_RefusesAnUnusableRepo covers the input
// guard. A denied probe is a permissions answer; a malformed repo is a
// caller mistake, and the two must not look alike — a caller that got
// ErrUsage knows to fix its argument, not to widen a token scope.
//
// The explicit empty-repo check inside ValidateBotPermissions is
// redundant: splitRepo refuses "" with the same sentinel, so removing
// the check changes nothing observable. The cases below assert the
// behaviour rather than which line produces it.
func TestValidateBotPermissions_RefusesAnUnusableRepo(t *testing.T) {
	t.Parallel()

	handler := permissionServer(http.StatusOK, http.StatusOK, http.StatusOK)

	p := &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
		HTTPClient:      inMemoryClient(handler),
		APIBaseOverride: "https://forgejo.invalid",
	}

	if _, err := p.ValidateBotPermissions(context.Background(), ""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty repo: err = %v, want ErrUsage", err)
	}

	for _, repo := range []string{"noslash", "owner/", "/repo"} {
		if _, err := p.ValidateBotPermissions(context.Background(), repo); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("repo %q: err = %v, want ErrUsage (a caller mistake, not a permissions answer)", repo, err)
		}
	}
}
