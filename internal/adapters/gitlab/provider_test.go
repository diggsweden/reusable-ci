// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitlabserver"
)

func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestProvider_Name(t *testing.T) {
	t.Parallel()

	if got := gitlab.New().Name(); got != provider.PlatformGitLab {
		t.Errorf("Name = %q", got)
	}
}

func TestResolveContext_BranchPush(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"CI_COMMIT_REF_NAME":  "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_COMMIT_BRANCH":    "main",
		"CI_COMMIT_SHA":       "abcdef0123456789abcdef0123456789abcdef01", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_COMMIT_SHORT_SHA": "abcdef0",
		"CI_PIPELINE_SOURCE":  "push", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_PROJECT_PATH":     "group/project",
		"CI_PROJECT_URL":      "https://gitlab.com/group/project",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypeBranch {
		t.Errorf("RefType = %q", evt.RefType)
	}

	if evt.RefName != "main" || evt.Branch != "main" {
		t.Errorf("RefName=%q Branch=%q", evt.RefName, evt.Branch)
	}

	if evt.Platform != provider.PlatformGitLab {
		t.Errorf("Platform = %q", evt.Platform)
	}
}

func TestResolveContext_TagPush(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"CI_COMMIT_REF_NAME": "v1.2.3",
		"CI_COMMIT_TAG":      "v1.2.3",
		"CI_PIPELINE_SOURCE": "push",
		"CI_COMMIT_SHA":      "abcdef0123456789",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypeTag {
		t.Errorf("RefType = %q, want tag", evt.RefType)
	}

	if evt.Branch != "" {
		t.Errorf("Branch should be empty on tag push, got %q", evt.Branch)
	}
}

func TestResolveContext_MergeRequest(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"CI_COMMIT_REF_NAME":                  "feat-x", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_PIPELINE_SOURCE":                  "merge_request_event",
		"CI_MERGE_REQUEST_IID":                "42",
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feat-x",
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypePR {
		t.Errorf("RefType = %q, want pr", evt.RefType)
	}

	if evt.PRNumber != "42" {
		t.Errorf("PRNumber = %q, want 42", evt.PRNumber)
	}

	if evt.Branch != "feat-x" {
		t.Errorf("Branch = %q (should fall back to MR source branch)", evt.Branch)
	}
}

func TestResolveContext_ShortSHAFallback(t *testing.T) {
	t.Parallel()
	// CI_COMMIT_SHORT_SHA absent → derive from first 7 of CI_COMMIT_SHA.
	p := &gitlab.Provider{Env: envFunc(map[string]string{
		"CI_COMMIT_SHA": "1234567890abcdef",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.ShortSHA != "1234567" {
		t.Errorf("ShortSHA fallback = %q", evt.ShortSHA)
	}
}

func TestResolveContext_EmptyEnv(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(nil)}

	evt, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if evt.RefType != provider.RefTypeOther {
		t.Errorf("RefType = %q, want Other on empty env", evt.RefType)
	}
}

func TestResolveContext_RefNameOnlyImpliesBranch(t *testing.T) {
	t.Parallel()
	// Detached-HEAD-ish pipelines populate CI_COMMIT_REF_NAME without
	// CI_COMMIT_BRANCH or CI_COMMIT_TAG. We treat that as a branch.
	p := &gitlab.Provider{Env: envFunc(map[string]string{
		"CI_COMMIT_REF_NAME": "detached",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypeBranch {
		t.Errorf("RefType = %q, want Branch (REF_NAME present, no TAG/BRANCH env)", evt.RefType)
	}
}

func TestResolveContext_MRTrumpsTag(t *testing.T) {
	t.Parallel()
	// CI_PIPELINE_SOURCE=merge_request_event always wins; tags should
	// not be possible during an MR pipeline but be defensive.
	p := &gitlab.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"CI_PIPELINE_SOURCE":   "merge_request_event",
		"CI_COMMIT_TAG":        "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_MERGE_REQUEST_IID": "1",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.RefType != provider.RefTypePR {
		t.Errorf("RefType = %q, want pr", evt.RefType)
	}
}

func TestFetchRepoMetadata_HappyPath(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	// GitLab URL-encodes the project path: "group/sub/project" → "group%2Fsub%2Fproject".
	srv.OnGet("/api/v4/projects/group%2Fsub%2Fproject", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "glpat_test" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			t.Errorf("PRIVATE-TOKEN header = %q", got)
		}

		return fakegitlabserver.Response{
			Body: `{"description":"a test project","web_url":"https://gitlab.com/group/sub/project","license":{"key":"apache-2.0","nickname":"Apache 2.0"}}`,
		}
	})
	p := &gitlab.Provider{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Env: envFunc(map[string]string{
			"GITLAB_TOKEN": "glpat_test", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}),
		APIBaseOverride: srv.URL(),
	}

	md, err := p.FetchRepoMetadata(context.Background(), "group/sub/project")
	if err != nil {
		t.Fatalf("FetchRepoMetadata: %v", err)
	}

	if md.Description != "a test project" {
		t.Errorf("Description = %q", md.Description)
	}

	if md.HTMLURL != "https://gitlab.com/group/sub/project" {
		t.Errorf("HTMLURL = %q", md.HTMLURL)
	}

	if md.LicenseSPDX != "apache-2.0" {
		t.Errorf("LicenseSPDX = %q", md.LicenseSPDX)
	}
}

func TestFetchRepoMetadata_FallsBackToCIJobToken(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/group%2Fproject", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "ci-job-token-abc" {
			t.Errorf("PRIVATE-TOKEN should fall back to CI_JOB_TOKEN, got %q", got)
		}

		return fakegitlabserver.Response{Body: `{}`}
	})

	p := &gitlab.Provider{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		Env: envFunc(map[string]string{
			"CI_JOB_TOKEN": "ci-job-token-abc",
		}),
		APIBaseOverride: srv.URL(),
	}
	if _, err := p.FetchRepoMetadata(context.Background(), "group/project"); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRepoMetadata_EmptyRepoNoCall(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	p := &gitlab.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	md, err := p.FetchRepoMetadata(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchRepoMetadata: %v", err)
	}

	if md.Description != "" || md.HTMLURL != "" || md.LicenseSPDX != "" {
		t.Errorf("expected zero metadata, got %+v", md)
	}

	if len(srv.Requests()) != 0 {
		t.Errorf("expected no HTTP calls, got %d", len(srv.Requests()))
	}
}

func TestFetchRepoMetadata_HTTPErrorPropagates(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/group%2Fproject", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 401, Body: `{"message":"401 Unauthorized"}`}
	})
	p := &gitlab.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	_, err := p.FetchRepoMetadata(context.Background(), "group/project")
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestValidateToken_HappyPath(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/group%2Fproject", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "glpat_test" {
			t.Errorf("PRIVATE-TOKEN = %q", got)
		}

		return fakegitlabserver.Response{Body: `{}`}
	})

	p := &gitlab.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}
	if err := p.ValidateToken(context.Background(), "glpat_test", "group/project"); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(nil)}
	if err := p.ValidateToken(context.Background(), "", "group/project"); err == nil {
		t.Fatal("expected empty-token error")
	}
}

func TestValidateBotPermissions_AllProbesPass(t *testing.T) {
	t.Parallel()

	srv := fakegitlabserver.New(t)
	for _, path := range []string{
		"/api/v4/user",
		"/api/v4/projects/group%2Fproject",
		"/api/v4/projects/group%2Fproject/repository/branches",
	} {
		srv.OnGet(path, func(_ fakegitlabserver.Request) fakegitlabserver.Response {
			return fakegitlabserver.Response{Body: `{}`}
		})
	}

	p := &gitlab.Provider{
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "glpat_test"}),
		APIBaseOverride: srv.URL(),
	}

	bp, err := p.ValidateBotPermissions(context.Background(), "group/project")
	if err != nil {
		t.Fatal(err)
	}

	if !bp.UserAccessible || !bp.RepoAccessible || !bp.BranchesAccessible {
		t.Errorf("BotPermissions = %+v", bp)
	}
}

func TestValidateBotPermissions_FallsBackToCIJobToken(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/user", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "ci-job-tok" {
			t.Errorf("PRIVATE-TOKEN = %q (should fall back to CI_JOB_TOKEN)", got)
		}

		return fakegitlabserver.Response{Body: `{}`}
	})
	srv.OnGet("/api/v4/projects/g%2Fp", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `{}`}
	})

	p := &gitlab.Provider{
		Env:             envFunc(map[string]string{"CI_JOB_TOKEN": "ci-job-tok"}),
		APIBaseOverride: srv.URL(),
	}
	if _, err := p.ValidateBotPermissions(context.Background(), "g/p"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRelease_PostsRelease(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	asset := writeTestAsset(t, "foo.zip", "zip bytes")

	srv.OnPost("/api/v4/projects/group%2Fproject/releases", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "glpat_test" {
			t.Errorf("token = %q", got)
		}

		if !strings.Contains(string(req.Body), `"tag_name":"v1.0.0"`) {
			t.Errorf("body missing tag_name: %s", req.Body)
		}

		return fakegitlabserver.Response{Status: 201, Body: `{}`}
	})
	srv.OnGet("/api/v4/projects/group%2Fproject/releases/v1.0.0/assets/links", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `[]`}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/uploads", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "glpat_test" {
			t.Errorf("upload token = %q", got)
		}

		if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("upload content-type = %q", req.Header.Get("Content-Type"))
		}

		if !strings.Contains(string(req.Body), "zip bytes") || !strings.Contains(string(req.Body), `filename="foo.zip"`) {
			t.Errorf("upload body missing file content/name: %s", req.Body)
		}

		return fakegitlabserver.Response{Status: 201, Body: `{"url":"/uploads/abc/foo.zip","full_path":"/group/project/uploads/abc/foo.zip"}`}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/releases/v1.0.0/assets/links", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		body := string(req.Body)
		if !strings.Contains(body, `"name":"foo.zip"`) || !strings.Contains(body, srv.URL()+`/group/project/uploads/abc/foo.zip`) {
			t.Errorf("link body missing name/url: %s", body)
		}

		return fakegitlabserver.Response{Status: 201, Body: `{}`}
	})
	p := &gitlab.Provider{
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "glpat_test"}),
		APIBaseOverride: srv.URL(),
	}

	err := p.CreateRelease(context.Background(), "group/project", provider.ReleaseSpec{
		Tag:    "v1.0.0",
		Name:   "Release v1.0.0",
		Assets: []string{asset},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUploadReleaseAsset_UploadsProjectFileAndLinksRelease(t *testing.T) {
	t.Parallel()

	srv := fakegitlabserver.New(t)
	asset := writeTestAsset(t, "artifact.tar.gz", "release asset")

	srv.OnGet("/api/v4/projects/group%2Fproject/releases/v1.2.3/assets/links", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if got := req.Header.Get("Private-Token"); got != "glpat_test" {
			t.Errorf("links token = %q", got)
		}

		return fakegitlabserver.Response{Body: `[]`}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/uploads", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("upload content-type = %q", req.Header.Get("Content-Type"))
		}

		if !strings.Contains(string(req.Body), "release asset") || !strings.Contains(string(req.Body), `filename="artifact.tar.gz"`) {
			t.Errorf("upload body missing file content/name: %s", req.Body)
		}

		return fakegitlabserver.Response{Status: 201, Body: `{"url":"/uploads/abc/artifact.tar.gz","full_path":"/group/project/uploads/abc/artifact.tar.gz"}`}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/releases/v1.2.3/assets/links", func(req fakegitlabserver.Request) fakegitlabserver.Response {
		body := string(req.Body)
		if !strings.Contains(body, `"name":"artifact.tar.gz"`) || !strings.Contains(body, srv.URL()+`/group/project/uploads/abc/artifact.tar.gz`) {
			t.Errorf("link body missing name/url: %s", body)
		}

		return fakegitlabserver.Response{Status: 201, Body: `{}`}
	})

	p := &gitlab.Provider{
		Env: envFunc(map[string]string{
			"GITLAB_TOKEN":    "glpat_test",
			"CI_PROJECT_PATH": "group/project",
		}),
		APIBaseOverride: srv.URL(),
	}

	if err := p.UploadReleaseAsset(context.Background(), "v1.2.3", asset); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}
}

func TestUploadReleaseAsset_ClobbersExistingLink(t *testing.T) {
	t.Parallel()

	srv := fakegitlabserver.New(t)
	asset := writeTestAsset(t, "artifact.tar.gz", "replacement")
	deleted := false

	srv.OnGet("/api/v4/projects/group%2Fproject/releases/v1.2.3/assets/links", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `[{"id":42,"name":"artifact.tar.gz"},{"id":7,"name":"other.txt"}]`}
	})
	srv.On("DELETE", "/api/v4/projects/group%2Fproject/releases/v1.2.3/assets/links/42", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		deleted = true

		return fakegitlabserver.Response{Status: 204}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/uploads", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{"full_path":"/group/project/uploads/def/artifact.tar.gz"}`}
	})
	srv.OnPost("/api/v4/projects/group%2Fproject/releases/v1.2.3/assets/links", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{}`}
	})

	p := &gitlab.Provider{
		Env: envFunc(map[string]string{
			"GITLAB_TOKEN":    "glpat_test",
			"CI_PROJECT_PATH": "group/project",
		}),
		APIBaseOverride: srv.URL(),
	}

	if err := p.UploadReleaseAsset(context.Background(), "v1.2.3", asset); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}

	if !deleted {
		t.Fatal("existing release asset link was not deleted")
	}
}

func TestUploadReleaseAsset_RequiresProjectPath(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: envFunc(nil)}

	err := p.UploadReleaseAsset(context.Background(), "v1.2.3", "asset.tar.gz")
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("UploadReleaseAsset error = %v, want ErrUsage", err)
	}
}

func TestCreateRelease_EmptyTagErrors(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{}

	err := p.CreateRelease(context.Background(), "group/p", provider.ReleaseSpec{})
	if err == nil {
		t.Fatal("expected empty-tag error")
	}
}

func TestCreateRelease_HTTP500Propagates(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnPost("/api/v4/projects/group%2Fproject/releases", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 500, Body: `{"message":"boom"}`}
	})
	p := &gitlab.Provider{Env: envFunc(nil), APIBaseOverride: srv.URL()}

	err := p.CreateRelease(context.Background(), "group/project", provider.ReleaseSpec{Tag: "v1.0.0"})
	if err == nil {
		t.Fatal("expected 500 error")
	}
}

func TestFetchRepoMetadata_ReadsCIServerURL(t *testing.T) {
	t.Parallel()
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/group%2Fproject", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `{}`}
	})
	// No APIBaseOverride — must read CI_SERVER_URL.
	p := &gitlab.Provider{Env: envFunc(map[string]string{"CI_SERVER_URL": srv.URL()})}
	if _, err := p.FetchRepoMetadata(context.Background(), "group/project"); err != nil {
		t.Fatal(err)
	}

	if len(srv.Requests()) != 1 {
		t.Errorf("expected 1 request, got %d", len(srv.Requests()))
	}
}

func writeTestAsset(t *testing.T, name, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
