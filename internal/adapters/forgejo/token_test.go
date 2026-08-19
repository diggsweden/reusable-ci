// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// permissionServer answers the three probe endpoints with the supplied
// status codes, so each permission can be denied independently.
func permissionServer(t *testing.T, user, repo, branches int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	t.Cleanup(srv.Close)

	return srv
}

// TestValidateBotPermissions_ReportsEachProbeIndependently covers a
// function with no test at all.
//
// The three flags exist so an operator can see *which* permission the bot
// token is missing. If one probe's outcome leaked into another the
// diagnostic would point at the wrong scope, which is worse than no
// diagnostic — someone would widen the wrong permission.
func TestValidateBotPermissions_ReportsEachProbeIndependently(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		user, repo, branches     int
		wantUser, wantRepo, want bool
	}{
		{
			name: "all granted",
			user: http.StatusOK, repo: http.StatusOK, branches: http.StatusOK,
			wantUser: true, wantRepo: true, want: true,
		},
		{
			// A token scoped to the repo but not to the user endpoint.
			name: "user denied",
			user: http.StatusForbidden, repo: http.StatusOK, branches: http.StatusOK,
			wantUser: false, wantRepo: true, want: true,
		},
		{
			name: "repo denied",
			user: http.StatusOK, repo: http.StatusNotFound, branches: http.StatusOK,
			wantUser: true, wantRepo: false, want: true,
		},
		{
			// The one that matters for pushing a release branch.
			name: "branches denied",
			user: http.StatusOK, repo: http.StatusOK, branches: http.StatusForbidden,
			wantUser: true, wantRepo: true, want: false,
		},
		{
			name: "all denied",
			user: http.StatusUnauthorized, repo: http.StatusUnauthorized, branches: http.StatusUnauthorized,
			wantUser: false, wantRepo: false, want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := permissionServer(t, tc.user, tc.repo, tc.branches)

			p := &forgejo.Provider{
				Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
				HTTPClient:      srv.Client(),
				APIBaseOverride: srv.URL,
			}

			got, err := p.ValidateBotPermissions(context.Background(), "owner/repo")
			if err != nil {
				t.Fatal(err)
			}

			if got.UserAccessible != tc.wantUser {
				t.Errorf("UserAccessible = %v, want %v", got.UserAccessible, tc.wantUser)
			}

			if got.RepoAccessible != tc.wantRepo {
				t.Errorf("RepoAccessible = %v, want %v", got.RepoAccessible, tc.wantRepo)
			}

			if got.BranchesAccessible != tc.want {
				t.Errorf("BranchesAccessible = %v, want %v", got.BranchesAccessible, tc.want)
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
	srv := permissionServer(t, http.StatusOK, http.StatusOK, http.StatusOK)

	p := &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}

	if _, err := p.ValidateBotPermissions(context.Background(), ""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty repo: err = %v, want ErrUsage", err)
	}

	for _, repo := range []string{"noslash", "owner/", "/repo"} {
		if _, err := p.ValidateBotPermissions(context.Background(), repo); err == nil {
			t.Errorf("repo %q was accepted", repo)
		}
	}
}
