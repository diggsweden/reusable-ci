// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// envMap returns a get-func over a fixed map for hermetic tests.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestName(t *testing.T) {
	t.Parallel()

	if got := forgejo.New().Name(); got != provider.PlatformForgejo {
		t.Errorf("Name() = %q, want forgejo", got)
	}
}

func TestResolveContext_PrefersForgejoEnvWithGitHubFallback(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: envMap(map[string]string{
		"FORGEJO_SHA":        "0123456789abcdef",
		"FORGEJO_REF_NAME":   "v1.2.3",
		"FORGEJO_REF_TYPE":   "tag",
		"FORGEJO_REPOSITORY": "itiquette/gommitlint",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
		"FORGEJO_EVENT_NAME": "push",
		// GITHUB_* fallback only used where FORGEJO_* is absent:
		"GITHUB_SHA": "should-not-win",
	})}

	ctx, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if ctx.Platform != provider.PlatformForgejo {
		t.Errorf("Platform = %q", ctx.Platform)
	}

	if ctx.SHA != "0123456789abcdef" || ctx.ShortSHA != "0123456" {
		t.Errorf("SHA=%q ShortSHA=%q", ctx.SHA, ctx.ShortSHA)
	}

	if ctx.RefType != provider.RefTypeTag || ctx.RefName != "v1.2.3" {
		t.Errorf("RefType=%q RefName=%q", ctx.RefType, ctx.RefName)
	}

	if ctx.Repo != "itiquette/gommitlint" || ctx.RepoURL != "https://codeberg.org/itiquette/gommitlint" {
		t.Errorf("Repo=%q RepoURL=%q", ctx.Repo, ctx.RepoURL)
	}
}

func TestResolveContext_GitHubFallback(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: envMap(map[string]string{
		"GITHUB_SHA":        "abc",
		"GITHUB_REF_NAME":   "main",
		"GITHUB_REF_TYPE":   "branch",
		"GITHUB_REPOSITORY": "owner/repo",
	})}

	ctx, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if ctx.RefType != provider.RefTypeBranch || ctx.Branch != "main" || ctx.Repo != "owner/repo" {
		t.Errorf("ctx = %+v", ctx)
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: envMap(map[string]string{"FORGEJO_SERVER_URL": "https://git.example.org"})}

	info := p.Describe()
	if info.DisplayName != "Forgejo" {
		t.Errorf("DisplayName = %q", info.DisplayName)
	}

	if info.SetupURL != "https://git.example.org/user/settings/applications" {
		t.Errorf("SetupURL = %q (should track the resolved server)", info.SetupURL)
	}

	if info.OIDCIssuer != "" {
		t.Errorf("OIDCIssuer = %q, want empty for now", info.OIDCIssuer)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	caps := forgejo.New().Capabilities()
	if caps.SARIFUpload {
		t.Error("Forgejo must not advertise SARIFUpload (no Code Scanning)")
	}

	if !caps.ReleaseAssets {
		t.Error("Forgejo should advertise ReleaseAssets")
	}
}

// newProvider wires a Provider at the test server with a fixed token.
func newProvider(srv *httptest.Server) *forgejo.Provider {
	return &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok", "FORGEJO_REPOSITORY": "itiquette/repo"}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}
}

func TestValidateToken_OK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo" {
			_, _ = w.Write([]byte(`{"description":"d","html_url":"https://codeberg.org/itiquette/repo"}`))

			return
		}

		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	defer srv.Close()

	if err := newProvider(srv).ValidateToken(context.Background(), "tok", "itiquette/repo"); err != nil {
		t.Fatalf("ValidateToken = %v", err)
	}
}

func TestValidateToken_Unauthorized(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := newProvider(srv).ValidateToken(context.Background(), "tok", "itiquette/repo")
	if err == nil {
		t.Fatal("expected error on 401")
	}

	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("want permission-denied class, got %v", err)
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	t.Parallel()

	err := forgejo.New().ValidateToken(context.Background(), "", "owner/repo")
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("empty token should be permission-denied, got %v", err)
	}
}

func TestFetchRepoMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"description":"a linter","html_url":"https://codeberg.org/itiquette/repo","object_format_name":"sha256"}`))
	}))
	defer srv.Close()

	md, err := newProvider(srv).FetchRepoMetadata(context.Background(), "itiquette/repo")
	if err != nil {
		t.Fatal(err)
	}

	if md.Description != "a linter" || md.HTMLURL != "https://codeberg.org/itiquette/repo" {
		t.Errorf("metadata = %+v", md)
	}

	// object_format_name feeds `platform checkout`'s git-init; it must
	// survive the mapping instead of needing the shell's curl+sed probe.
	if md.ObjectFormat != "sha256" {
		t.Errorf("ObjectFormat = %q, want sha256", md.ObjectFormat)
	}
}

func TestCreateRelease_CreatesAndUploadsAssets(t *testing.T) {
	t.Parallel()

	asset := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(asset, []byte("deadbeef  artifact\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var (
		createdRelease bool
		uploadedAsset  bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// No existing release at this tag.
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/releases/tags/"):
			http.Error(w, "not found", http.StatusNotFound)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/releases"):
			createdRelease = true

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42,"tag_name":"v1.0.0"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/releases/42/assets"):
			uploadedAsset = true

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"name":"checksums.txt"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	err := newProvider(srv).CreateRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:    "v1.0.0",
		Name:   "v1.0.0",
		Assets: []string{asset},
	})
	if err != nil {
		t.Fatalf("CreateRelease = %v", err)
	}

	if !createdRelease || !uploadedAsset {
		t.Errorf("createdRelease=%v uploadedAsset=%v", createdRelease, uploadedAsset)
	}
}

func TestCreateRelease_EmptyTag(t *testing.T) {
	t.Parallel()

	err := forgejo.New().CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty tag should be usage error, got %v", err)
	}
}
