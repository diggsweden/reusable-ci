// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"encoding/json"
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

func TestName_IsForgejo(t *testing.T) {
	t.Parallel()

	if got := forgejo.New().Name(); got != provider.ForgeForgejo {
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

	if ctx.ForgeAPI != provider.ForgeForgejo {
		t.Errorf("Platform = %q", ctx.ForgeAPI)
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

func TestDescribe_ReportsDisplayNameAndServerURL(t *testing.T) {
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

func TestCapabilities_AdvertisesReleaseAssetsButNotSARIF(t *testing.T) {
	t.Parallel()

	caps := forgejo.New().Capabilities()
	if caps.SARIFUpload {
		t.Error("Forgejo must not advertise SARIFUpload (no Code Scanning)")
	}

	if !caps.ReleaseAssets {
		t.Error("Forgejo should advertise ReleaseAssets")
	}

	if !caps.RunArtifacts {
		t.Error("Forgejo should advertise RunArtifacts (in-run Actions runtime service)")
	}
}

// newProvider wires a Provider at the test server with a fixed token.
// newProvider serves handler in memory: each request is answered through a
// recorder, so no listener is opened and the handler sees exactly what the
// adapter sent.
func newProvider(handler http.Handler) *forgejo.Provider {
	return &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok", "FORGEJO_REPOSITORY": "itiquette/repo"}),
		HTTPClient:      inMemoryClient(handler),
		APIBaseOverride: "https://forgejo.invalid",
	}
}

// inMemoryClient answers every request with handler through a recorder.
func inMemoryClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		response := recorder.Result()
		response.Request = req

		return response, nil
	})}
}

func TestValidateToken_OK(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo" {
			_, _ = w.Write([]byte(`{"description":"d","html_url":"https://codeberg.org/itiquette/repo"}`))

			return
		}

		http.Error(w, "unexpected", http.StatusNotFound)
	})

	if err := newProvider(handler).ValidateToken(context.Background(), "tok", "itiquette/repo"); err != nil {
		t.Fatalf("ValidateToken = %v", err)
	}
}

func TestValidateToken_Unauthorized(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	})

	err := newProvider(handler).ValidateToken(context.Background(), "tok", "itiquette/repo")
	if err == nil {
		t.Fatal("expected error on 401")
	}

	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("want permission-denied class, got %v", err)
	}
}

// TestValidateToken_StatusKeepsItsExitClass pins what a caller scripts
// against: a refused credential exits no-permission (77) and a repository the
// token cannot see exits no-input (66), matching the GitLab adapter.
func TestValidateToken_StatusKeepsItsExitClass(t *testing.T) {
	t.Parallel()

	for status, want := range map[int]errs.ExitCodeType{401: errs.ExitCodeNoPerm, 403: errs.ExitCodeNoPerm, 404: errs.ExitCodeNoInput} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "refused", status)
		})

		err := newProvider(handler).ValidateToken(context.Background(), "synthetic-forgejo-secret", "itiquette/repo")
		if got := errs.ExitCodeFromError(err); got != want {
			t.Errorf("HTTP %d exits %d, want %d", status, got, want)
		}

		if strings.Contains(err.Error(), "synthetic-forgejo-secret") {
			t.Errorf("HTTP %d refusal echoes the token: %v", status, err)
		}
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	t.Parallel()

	err := forgejo.New().ValidateToken(context.Background(), "", "owner/repo")
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("empty token should be permission-denied, got %v", err)
	}
}

func TestFetchRepoMetadata_ReadsDescriptionURLAndObjectFormat(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"description":"a linter","html_url":"https://codeberg.org/itiquette/repo","object_format_name":"sha256"}`))
	})

	md, err := newProvider(handler).FetchRepoMetadata(context.Background(), "itiquette/repo")
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	err := newProvider(handler).CreateRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
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

func TestPublishRelease_UpdatesExistingReleaseAndReconcilesAssets(t *testing.T) {
	t.Parallel()

	asset := filepath.Join(t.TempDir(), "asset.tgz")
	if err := os.WriteFile(asset, []byte("asset\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	notes := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(notes, []byte("release notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/tags/v1.2.3":
			_, _ = w.Write([]byte(`{"id":99,"tag_name":"v1.2.3","assets":[{"id":1,"name":"asset.tgz"},{"id":2,"name":"stale.txt"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode patch payload: %v", err)
			}

			if payload["name"] != "repo v1.2.3" || payload["body"] != "release notes\n" || payload["draft"] != false || payload["prerelease"] != false {
				t.Fatalf("patch payload = %#v", payload)
			}

			_, _ = w.Write([]byte(`{"id":99,"tag_name":"v1.2.3"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets":
			_, _ = w.Write([]byte(`[{"id":1,"name":"asset.tgz"},{"id":2,"name":"stale.txt"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets/1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets/2":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/99/assets":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"name":"asset.tgz"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	})

	err := newProvider(handler).PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:       "v1.2.3",
		Name:      "repo v1.2.3",
		NotesFile: notes,
		Draft:     false,
		Assets:    []string{asset},
	})
	if err != nil {
		t.Fatalf("PublishRelease = %v", err)
	}

	assertCallPresent(t, calls, "PATCH /api/v1/repos/itiquette/repo/releases/99")
	assertCallPresent(t, calls, "DELETE /api/v1/repos/itiquette/repo/releases/99/assets/1")
	assertCallPresent(t, calls, "POST /api/v1/repos/itiquette/repo/releases/99/assets")
	assertCallPresent(t, calls, "DELETE /api/v1/repos/itiquette/repo/releases/99/assets/2")
	assertCallAbsent(t, calls, "DELETE /api/v1/repos/itiquette/repo/releases/99")

	// Replacement uploads before it deletes, for both the colliding asset and
	// the stale one. Deleting first would mean a failed upload leaves the
	// release with neither the old asset nor the new one; uploading first can
	// at worst leave two, and a delete that fails is reported rather than
	// leaving that duplicate unremarked. Losing a published asset is the worse
	// outcome of the two, so this is the order.
	if indexCall(calls, "DELETE /api/v1/repos/itiquette/repo/releases/99/assets/1") < indexCall(calls, "POST /api/v1/repos/itiquette/repo/releases/99/assets") {
		t.Fatalf("colliding asset was deleted before its replacement uploaded: %v", calls)
	}

	if indexCall(calls, "DELETE /api/v1/repos/itiquette/repo/releases/99/assets/2") < indexCall(calls, "POST /api/v1/repos/itiquette/repo/releases/99/assets") {
		t.Fatalf("stale asset was deleted before desired upload succeeded: %v", calls)
	}
}

func TestPublishRelease_CreatesMissingRelease(t *testing.T) {
	t.Parallel()

	asset := filepath.Join(t.TempDir(), "asset.tgz")
	if err := os.WriteFile(asset, []byte("asset\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	notes := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(notes, []byte("release notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/tags/v1.2.3":
			http.Error(w, "not found", http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/itiquette/repo/releases":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create payload: %v", err)
			}

			if payload["name"] != "repo v1.2.3" || payload["body"] != "release notes\n" || payload["draft"] != true {
				t.Fatalf("create payload = %#v", payload)
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42,"tag_name":"v1.2.3"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/itiquette/repo/releases/42/assets":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"name":"asset.tgz"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	})

	err := newProvider(handler).PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:       "v1.2.3",
		Name:      "repo v1.2.3",
		NotesFile: notes,
		Draft:     true,
		Assets:    []string{asset},
	})
	if err != nil {
		t.Fatalf("PublishRelease = %v", err)
	}

	assertCallPresent(t, calls, "POST /api/v1/repos/itiquette/repo/releases")
	assertCallPresent(t, calls, "POST /api/v1/repos/itiquette/repo/releases/42/assets")
	assertCallAbsent(t, calls, "PATCH /api/v1/repos/itiquette/repo/releases/42")
	assertCallAbsent(t, calls, "DELETE /api/v1/repos/itiquette/repo/releases/42")
}

func TestPublishRelease_EmptyTag(t *testing.T) {
	t.Parallel()

	err := forgejo.New().PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty tag should be usage error, got %v", err)
	}
}

func assertCallPresent(t *testing.T, calls []string, want string) {
	t.Helper()

	if indexCall(calls, want) < 0 {
		t.Fatalf("missing call %q in %v", want, calls)
	}
}

func assertCallAbsent(t *testing.T, calls []string, want string) {
	t.Helper()

	if indexCall(calls, want) >= 0 {
		t.Fatalf("unexpected call %q in %v", want, calls)
	}
}

func indexCall(calls []string, want string) int {
	for i, call := range calls {
		if call == want {
			return i
		}
	}

	return -1
}

// TestCreateRelease_UnreadableNotesFileFailsBeforeAnyRequest pins that a
// notes file that cannot be read fails the create instead of shipping the
// release with its name as the body, matching GitHub and PublishRelease.
func TestCreateRelease_UnreadableNotesFileFailsBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	requests := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++

		http.Error(w, "unexpected", http.StatusInternalServerError)
	})

	err := newProvider(handler).CreateRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:       "v1.0.0",
		Name:      "v1.0.0",
		NotesFile: filepath.Join(t.TempDir(), "missing-notes.md"),
	})
	if err == nil || !strings.Contains(err.Error(), "release notes") {
		t.Fatalf("CreateRelease = %v, want a read-notes failure", err)
	}

	if requests != 0 {
		t.Errorf("%d request(s) were sent although the notes could not be read", requests)
	}
}

// TestCreateRelease_LookupFailureIsNotTreatedAsAbsent pins that only a 404
// on the existing-release lookup means "nothing to replace": an outage or
// auth failure there is reported, not read as an absent release.
func TestCreateRelease_LookupFailureIsNotTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	created := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/releases/tags/"):
			http.Error(w, "forbidden", http.StatusForbidden)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/releases"):
			created = true

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42,"tag_name":"v1.0.0"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	})

	err := newProvider(handler).CreateRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{Tag: "v1.0.0", Name: "v1.0.0"})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("CreateRelease = %v, want the lookup's ErrPermissionDenied", err)
	}

	if created {
		t.Error("a release was created although the existing-release lookup failed")
	}
}
