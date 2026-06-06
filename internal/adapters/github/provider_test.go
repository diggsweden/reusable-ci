// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakegitserver"
)

// envFunc returns a fixed-map env getter. Empty when not present.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestProvider_Name(t *testing.T) {
	t.Parallel()

	p := github.New()
	if got := p.Name(); got != provider.PlatformGitHub {
		t.Errorf("Name = %q, want %q", got, provider.PlatformGitHub)
	}
}

//nolint:cyclop // verifies many resolved EventContext fields on one fixture.
func TestResolveContext_BranchPush(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"GITHUB_REF":        "refs/heads/main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_REF_NAME":   "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_REF_TYPE":   "branch", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_SHA":        "abcdef0123456789abcdef0123456789abcdef01", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_EVENT_NAME": "push", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_REPOSITORY": "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"GITHUB_SERVER_URL": "https://github.com",
	})}

	evt, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if evt.RefName != "main" || evt.RefType != provider.RefTypeBranch {
		t.Errorf("RefName=%q RefType=%q", evt.RefName, evt.RefType)
	}

	if evt.SHA != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("SHA = %q", evt.SHA)
	}

	if evt.ShortSHA != "abcdef0" {
		t.Errorf("ShortSHA = %q, want abcdef0", evt.ShortSHA)
	}

	if evt.Branch != "main" {
		t.Errorf("Branch = %q, want main", evt.Branch)
	}

	if evt.PRNumber != "" {
		t.Errorf("PRNumber = %q, want empty on push", evt.PRNumber)
	}

	if evt.Repo != "owner/repo" || evt.RepoURL != "https://github.com/owner/repo" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("Repo=%q URL=%q", evt.Repo, evt.RepoURL)
	}

	if evt.Platform != provider.PlatformGitHub {
		t.Errorf("Platform = %q", evt.Platform)
	}
}

func TestResolveContext_TagPush(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"GITHUB_REF":        "refs/tags/v1.2.3",
		"GITHUB_REF_NAME":   "v1.2.3",
		"GITHUB_REF_TYPE":   "tag",
		"GITHUB_EVENT_NAME": "push",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypeTag {
		t.Errorf("RefType = %q, want tag", evt.RefType)
	}

	if evt.Branch != "" {
		t.Errorf("Branch should be empty on tag push, got %q", evt.Branch)
	}
}

func TestResolveContext_PullRequest(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"GITHUB_REF":        "refs/pull/42/merge",
		"GITHUB_REF_NAME":   "42/merge",
		"GITHUB_REF_TYPE":   "branch", // GitHub reports branch even on PR
		"GITHUB_HEAD_REF":   "feat/x",
		"GITHUB_EVENT_NAME": "pull_request",
		"GITHUB_SHA":        "deadbeef",
		"GITHUB_REPOSITORY": "owner/repo",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypePR {
		t.Errorf("RefType = %q, want pr", evt.RefType)
	}

	if evt.PRNumber != "42" {
		t.Errorf("PRNumber = %q, want 42", evt.PRNumber)
	}

	if evt.Branch != "feat/x" {
		t.Errorf("Branch = %q, want feat/x (HEAD_REF on PR)", evt.Branch)
	}
}

func TestResolveContext_PullRequestTarget(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{
		"GITHUB_REF":        "refs/pull/100/merge",
		"GITHUB_EVENT_NAME": "pull_request_target",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypePR {
		t.Errorf("RefType = %q, want pr (pull_request_target should also be PR)", evt.RefType)
	}

	if evt.PRNumber != "100" {
		t.Errorf("PRNumber = %q, want 100", evt.PRNumber)
	}
}

func TestResolveContext_DefaultsServerURL(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{
		"GITHUB_REPOSITORY": "owner/repo",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q, want default github.com", evt.RepoURL)
	}
}

func TestResolveContext_EnterpriseServerURL(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{
		"GITHUB_REPOSITORY": "myorg/myrepo",
		"GITHUB_SERVER_URL": "https://github.acme.example",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RepoURL != "https://github.acme.example/myorg/myrepo" {
		t.Errorf("RepoURL = %q", evt.RepoURL)
	}
}

func TestResolveContext_EmptyEnv(t *testing.T) {
	t.Parallel()
	// No env set — RefType falls through to Other.
	p := &github.Provider{Env: envFunc(nil)}

	evt, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if evt.RefType != provider.RefTypeOther {
		t.Errorf("RefType = %q, want Other", evt.RefType)
	}

	if evt.Platform != provider.PlatformGitHub {
		t.Errorf("Platform = %q", evt.Platform)
	}
}

func TestResolveContext_ShortSHATruncates(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{
		"GITHUB_SHA": "1234567890",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.ShortSHA != "1234567" {
		t.Errorf("ShortSHA = %q, want first 7", evt.ShortSHA)
	}
}

func TestFetchRepoMetadata_HappyPath(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(req fakegitserver.Request) fakegitserver.Response {
		// Verify auth + content-type headers are forwarded.
		if got := req.Header.Get("Authorization"); got != "Bearer ghs_test" {
			t.Errorf("Authorization header = %q", got)
		}

		if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept header = %q", got)
		}

		if got := req.Header.Get("X-Github-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version header = %q", got)
		}

		return fakegitserver.Response{
			Body: `{"description":"a test repo","html_url":"https://github.com/owner/repo","license":{"spdx_id":"Apache-2.0"}}`,
		}
	})
	p := &github.Provider{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Env: envFunc(map[string]string{
			"GITHUB_TOKEN": "ghs_test", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}),
		APIBaseOverride: srv.URL(),
	}

	md, err := p.FetchRepoMetadata(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("FetchRepoMetadata: %v", err)
	}

	if md.Description != "a test repo" {
		t.Errorf("Description = %q", md.Description)
	}

	if md.HTMLURL != "https://github.com/owner/repo" {
		t.Errorf("HTMLURL = %q", md.HTMLURL)
	}

	if md.LicenseSPDX != "Apache-2.0" {
		t.Errorf("LicenseSPDX = %q", md.LicenseSPDX)
	}
}

func TestFetchRepoMetadata_EmptyRepoNoCall(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	// Intentionally no route — if the adapter calls anyway, the 404 will
	// surface as an error and fail this test.
	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	md, err := p.FetchRepoMetadata(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchRepoMetadata: %v", err)
	}

	if md.Description != "" || md.HTMLURL != "" || md.LicenseSPDX != "" {
		t.Errorf("expected zero metadata for empty repo, got %+v", md)
	}

	if len(srv.Requests()) != 0 {
		t.Errorf("expected no HTTP calls, got %d", len(srv.Requests()))
	}
}

func TestFetchRepoMetadata_OmitAuthWhenNoToken(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(req fakegitserver.Request) fakegitserver.Response {
		if got := req.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header should be empty without token, got %q", got)
		}

		return fakegitserver.Response{Body: `{"description":"public"}`}
	})
	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	md, err := p.FetchRepoMetadata(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("FetchRepoMetadata: %v", err)
	}

	if md.Description != "public" {
		t.Errorf("Description = %q", md.Description)
	}
}

func TestFetchRepoMetadata_HTTPErrorPropagates(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: 404, Body: `{"message":"Not Found"}`}
	})
	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	_, err := p.FetchRepoMetadata(context.Background(), "owner/repo")
	if err == nil {
		t.Fatal("expected error on 404")
	}
}

func TestValidateToken_HappyPath(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(req fakegitserver.Request) fakegitserver.Response {
		if got := req.Header.Get("Authorization"); got != "Bearer github_pat_AAAA" {
			t.Errorf("Authorization = %q", got)
		}

		return fakegitserver.Response{Body: `{}`}
	})

	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}
	if err := p.ValidateToken(context.Background(), "github_pat_AAAA", "owner/repo"); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
}

func TestValidateToken_EmptyTokenError(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(nil)}

	err := p.ValidateToken(context.Background(), "", "owner/repo")
	if err == nil {
		t.Fatal("expected error on empty token")
	}
}

func TestValidateToken_HTTP403(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: 403, Body: `{"message":"forbidden"}`}
	})
	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	err := p.ValidateToken(context.Background(), "github_pat_AAAA", "owner/repo")
	if err == nil {
		t.Fatal("expected 403 error")
	}
}

func TestValidateBotPermissions_AllProbesPass(t *testing.T) {
	t.Parallel()

	srv := fakegitserver.New(t)
	for _, path := range []string{"/user", "/repos/owner/repo", "/repos/owner/repo/branches"} {
		srv.OnGet(path, func(_ fakegitserver.Request) fakegitserver.Response {
			return fakegitserver.Response{Body: `{}`}
		})
	}

	p := &github.Provider{Env: envFunc(map[string]string{"GITHUB_TOKEN": "ghs_AAA"}), APIBaseOverride: srv.URL()}

	bp, err := p.ValidateBotPermissions(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}

	if !bp.UserAccessible || !bp.RepoAccessible || !bp.BranchesAccessible {
		t.Errorf("BotPermissions = %+v, want all true", bp)
	}
}

func TestValidateBotPermissions_BranchesProbeFailsWarn(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/user", func(_ fakegitserver.Request) fakegitserver.Response { return fakegitserver.Response{Body: `{}`} })
	srv.OnGet("/repos/owner/repo", func(_ fakegitserver.Request) fakegitserver.Response { return fakegitserver.Response{Body: `{}`} })
	// /branches not registered → 404.
	p := &github.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	bp, err := p.ValidateBotPermissions(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}

	if !bp.UserAccessible || !bp.RepoAccessible {
		t.Errorf("user/repo should be accessible, got %+v", bp)
	}

	if bp.BranchesAccessible {
		t.Error("branches probe should report false")
	}
}

func TestFetchRepoMetadata_ReadsAPIBaseFromEnv(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Body: `{}`}
	})
	// No APIBaseOverride — the adapter must read GITHUB_API_URL.
	p := &github.Provider{Env: envFunc(map[string]string{"GITHUB_API_URL": srv.URL()})}
	if _, err := p.FetchRepoMetadata(context.Background(), "owner/repo"); err != nil {
		t.Fatal(err)
	}

	if len(srv.Requests()) != 1 {
		t.Errorf("expected 1 request, got %d", len(srv.Requests()))
	}
}
