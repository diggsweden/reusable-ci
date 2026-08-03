// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// recordedCall is one HTTP interaction observed by the fake API.
type recordedCall struct {
	Method string
	Path   string
	Body   string
}

// fakeGitHub serves a tiny subset of the GitHub REST surface: get release
// by tag, create release, delete release, list assets, delete asset,
// upload asset. Tests configure responses per path and inspect the
// recorded call log to assert behaviour.
type fakeGitHub struct {
	mu       sync.Mutex
	calls    []recordedCall
	releases map[string]*recordedRelease
	nextID   int64
}

type recordedRelease struct {
	ID         int64
	TagName    string
	Name       string
	Draft      bool
	Prerelease bool
	MakeLatest string
	Body       string
	Assets     []*recordedAsset
}

type recordedAsset struct {
	ID   int64
	Name string
	Body string
}

func newFake() *fakeGitHub {
	return &fakeGitHub{releases: map[string]*recordedRelease{}, nextID: 1000}
}

func (f *fakeGitHub) Calls() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]recordedCall, len(f.calls))
	copy(out, f.calls)

	return out
}

func (f *fakeGitHub) preloadRelease(draft, prerelease bool) *recordedRelease {
	f.mu.Lock()
	defer f.mu.Unlock()

	const tag = "v1.0.0" // every preload uses the same fixture tag

	f.nextID++
	rel := &recordedRelease{ID: f.nextID, TagName: tag, Draft: draft, Prerelease: prerelease}
	f.releases[tag] = rel

	return rel
}

//nolint:cyclop,gocognit // test fixture mux dispatches every method/path the GitHub adapter touches.
func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/tags/", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))
		tag := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/")

		f.mu.Lock()
		rel, ok := f.releases[tag]
		f.mu.Unlock()

		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

			return
		}

		writeRelease(w, rel)
	})
	mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

			return
		}

		var in struct {
			TagName    string `json:"tag_name"`
			Name       string `json:"name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
			MakeLatest string `json:"make_latest"`
			Body       string `json:"body"`
		}

		_ = json.Unmarshal(body, &in)

		f.mu.Lock()
		f.nextID++
		rel := &recordedRelease{
			ID:         f.nextID,
			TagName:    in.TagName,
			Name:       in.Name,
			Draft:      in.Draft,
			Prerelease: in.Prerelease,
			MakeLatest: in.MakeLatest,
			Body:       in.Body,
		}
		f.releases[in.TagName] = rel
		f.mu.Unlock()
		writeRelease(w, rel)
	})
	mux.HandleFunc("/repos/owner/repo/releases/", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))

		path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/")
		switch {
		case strings.HasSuffix(path, "/assets"):
			f.mu.Lock()

			var assets []*recordedAsset

			id := releaseIDFromPath(path)
			for _, rel := range f.releases {
				if rel.ID == id {
					assets = rel.Assets

					break
				}
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(assetsToWire(assets))
		case strings.Contains(path, "/assets/") && r.Method == http.MethodDelete:
			id := assetIDFromPath(path)

			f.mu.Lock()
			for _, rel := range f.releases {
				rel.Assets = filterOutAsset(rel.Assets, id)
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch:
			id := releaseIDFromPath(path)

			var in struct {
				TagName    string `json:"tag_name"`
				Name       string `json:"name"`
				Draft      bool   `json:"draft"`
				Prerelease bool   `json:"prerelease"`
				MakeLatest string `json:"make_latest"`
				Body       string `json:"body"`
			}

			_ = json.Unmarshal(body, &in)

			f.mu.Lock()
			for _, rel := range f.releases {
				if rel.ID == id {
					rel.Name, rel.Draft, rel.Prerelease = in.Name, in.Draft, in.Prerelease
					rel.MakeLatest, rel.Body = in.MakeLatest, in.Body
					f.mu.Unlock()
					writeRelease(w, rel)

					return
				}
			}
			f.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
		case r.Method == http.MethodDelete:
			id := releaseIDFromPath(path)

			f.mu.Lock()
			for tag, rel := range f.releases {
				if rel.ID == id {
					delete(f.releases, tag)

					break
				}
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	return mux
}

// uploadHandler handles the asset upload endpoint, which lives on a
// separate host in production but on the same httptest server here.
func (f *fakeGitHub) uploadHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))

		path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/")
		if !strings.HasSuffix(path, "/assets") || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)

			return
		}

		releaseID := releaseIDFromPath(path)
		name := r.URL.Query().Get("name")

		f.mu.Lock()
		f.nextID++

		a := &recordedAsset{ID: f.nextID, Name: name, Body: string(body)} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		for _, rel := range f.releases {
			if rel.ID == releaseID {
				rel.Assets = append(rel.Assets, a)

				break
			}
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": a.ID, "name": a.Name}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})

	return mux
}

func (f *fakeGitHub) record(r *http.Request, body string) {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{Method: r.Method, Path: r.URL.Path, Body: body})
	f.mu.Unlock()
}

func writeRelease(w http.ResponseWriter, rel *recordedRelease) {
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(map[string]any{
		"id":          rel.ID,
		"tag_name":    rel.TagName,
		"name":        rel.Name,
		"draft":       rel.Draft,
		"prerelease":  rel.Prerelease,
		"make_latest": rel.MakeLatest,
		"body":        rel.Body,
	}); err != nil {
		panic(err) // test fixture; payload is fully typed-safe
	}
}

func assetsToWire(assets []*recordedAsset) []map[string]any {
	out := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		out = append(out, map[string]any{"id": a.ID, "name": a.Name})
	}

	return out
}

func releaseIDFromPath(path string) int64 {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0
	}

	var id int64

	_, _ = fmt.Sscanf(parts[0], "%d", &id)

	return id
}

func assetIDFromPath(path string) int64 {
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		return 0
	}

	var id int64

	_, _ = fmt.Sscanf(parts[2], "%d", &id)

	return id
}

func filterOutAsset(assets []*recordedAsset, id int64) []*recordedAsset {
	out := assets[:0]
	for _, a := range assets {
		if a.ID != id {
			out = append(out, a)
		}
	}

	return out
}

// combinedHandler folds api + upload routes onto the same httptest
// server. Production uses api.github.com + uploads.github.com; the
// adapter's APIBaseOverride is set to one URL for both.
func combinedHandler(f *fakeGitHub) http.Handler {
	apiH := f.handler()
	uploadH := f.uploadHandler()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/assets") {
			uploadH.ServeHTTP(w, r)

			return
		}

		apiH.ServeHTTP(w, r)
	})
}

func providerForFake(srv *httptest.Server) *github.Provider {
	return &github.Provider{
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
		Env: func(k string) string {
			switch k {
			case "GH_TOKEN", "GITHUB_TOKEN": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				return "test-token"
			case "GITHUB_REPOSITORY": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				return "owner/repo" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			}

			return ""
		},
	}
}

func callMethods(calls []recordedCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method)
	}

	return out
}

func equalPrefix(got, want []string) bool {
	if len(got) < len(want) {
		return false
	}

	for i, w := range want {
		if got[i] != w {
			return false
		}
	}

	return true
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}

	return false
}

func TestCreateRelease_NoExistingRelease(t *testing.T) {
	fake := newFake()

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)

	if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Name:       "v1.0.0",
		MakeLatest: provider.MakeLatestTrue,
	}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	calls := fake.Calls()
	if len(calls) < 2 {
		t.Fatalf("calls = %v, want at least probe + create", calls)
	}

	if calls[0].Method != http.MethodGet || !strings.Contains(calls[0].Path, "/releases/tags/v1.0.0") {
		t.Errorf("first call = %+v, want GET releases/tags/v1.0.0", calls[0])
	}

	if calls[1].Method != http.MethodPost || calls[1].Path != "/repos/owner/repo/releases" {
		t.Errorf("second call = %+v, want POST releases", calls[1])
	}

	for _, c := range calls {
		if c.Method == http.MethodDelete {
			t.Errorf("did not expect DELETE on no-existing-release path: %+v", c)
		}
	}
}

func TestCreateRelease_DeletesExistingDraftOrPrerelease(t *testing.T) {
	for _, tc := range []struct {
		name       string
		draft      bool
		prerelease bool
	}{
		{name: "draft", draft: true},
		{name: "prerelease", prerelease: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFake()
			fake.preloadRelease(tc.draft, tc.prerelease)

			srv := httptest.NewServer(fake.handler())
			defer srv.Close()

			p := providerForFake(srv)

			if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
				Tag:        "v1.0.0",
				MakeLatest: provider.MakeLatestTrue,
			}); err != nil {
				t.Fatalf("CreateRelease: %v", err)
			}

			methods := callMethods(fake.Calls())
			if got := strings.Join(methods, ","); !strings.Contains(got, "GET,DELETE,POST") {
				t.Errorf("expected GET→DELETE→POST sequence, got %s", got)
			}
		})
	}
}

func TestCreateRelease_RefusesExistingStable(t *testing.T) {
	fake := newFake()
	fake.preloadRelease(false, false)

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)

	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0",
		MakeLatest: provider.MakeLatestTrue,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot overwrite") {
		t.Fatalf("err = %v, want refusal", err)
	}

	for _, c := range fake.Calls() {
		if c.Method == http.MethodDelete {
			t.Errorf("must not delete a stable release: %+v", c)
		}
	}
}

func TestCreateRelease_PostsDraftPrereleaseAndMakeLatestFlags(t *testing.T) {
	fake := newFake()

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)

	if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v2.0.0",
		Name:       "Major",
		Draft:      true,
		Prerelease: true,
		MakeLatest: provider.MakeLatestFalse,
	}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	for _, c := range fake.Calls() {
		if c.Method == http.MethodPost {
			for _, want := range []string{`"draft":true`, `"prerelease":true`, `"make_latest":"false"`, `"tag_name":"v2.0.0"`, `"name":"Major"`} {
				if !strings.Contains(c.Body, want) {
					t.Errorf("POST body missing %q\nbody: %s", want, c.Body)
				}
			}

			return
		}
	}

	t.Fatalf("no POST observed: %+v", fake.Calls())
}

func TestCreateRelease_EmptyTagErrors(t *testing.T) {
	p := &github.Provider{}

	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if err == nil || !strings.Contains(err.Error(), "tag is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateRelease_ForwardsNotesFileBody(t *testing.T) {
	fake := newFake()

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	dir := t.TempDir()

	notesPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(notesPath, []byte("# Release notes\n\nfoo"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v3.0.0",
		Name:       "v3.0.0",
		NotesFile:  notesPath,
		MakeLatest: provider.MakeLatestTrue,
	}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	for _, c := range fake.Calls() {
		if c.Method == http.MethodPost && strings.Contains(c.Body, `"body":"# Release notes\n\nfoo"`) {
			return
		}
	}

	t.Fatalf("expected POST body to carry release notes, got: %+v", fake.Calls())
}

func TestPublishRelease_CreatesWhenMissing(t *testing.T) {
	fake := newFake()

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	p := providerForFake(srv)

	if err := p.PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:  "v1.0.0",
		Name: "v1.0.0",
	}); err != nil {
		t.Fatalf("PublishRelease: %v", err)
	}

	methods := callMethods(fake.Calls())
	if !equalPrefix(methods, []string{"GET", "POST"}) {
		t.Errorf("methods = %v, want GET,POST prefix (probe then create)", methods)
	}

	for _, c := range fake.Calls() {
		if c.Method == http.MethodDelete || c.Method == http.MethodPatch {
			t.Errorf("must not edit or delete when the release is absent: %+v", c)
		}
	}
}

func TestPublishRelease_UpdatesInPlaceAndReconcilesAssets(t *testing.T) {
	fake := newFake()
	rel := fake.preloadRelease(false, false) // a stable release CreateRelease would refuse
	rel.Assets = append(rel.Assets, &recordedAsset{ID: 555, Name: "stale.tgz"})

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	p := providerForFake(srv)

	dir := t.TempDir()

	asset := filepath.Join(dir, "new.tgz")
	if err := os.WriteFile(asset, []byte("payload"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := p.PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:    "v1.0.0",
		Name:   "renamed",
		Assets: []string{asset},
	}); err != nil {
		t.Fatalf("PublishRelease: %v", err)
	}

	calls := fake.Calls()
	methods := callMethods(calls)

	if !contains(methods, "PATCH") {
		t.Errorf("expected in-place PATCH edit of the existing release, got %v", methods)
	}

	// The release object itself must never be deleted (that is the recreate
	// strategy, not reconcile).
	releasePath := fmt.Sprintf("/repos/owner/repo/releases/%d", rel.ID)
	for _, c := range calls {
		if c.Method == http.MethodDelete && c.Path == releasePath {
			t.Errorf("reconcile must not delete the release object: %+v", c)
		}
	}

	if !contains(methods, "POST") {
		t.Errorf("expected the desired asset to be uploaded (POST), got %v", methods)
	}

	if !contains(methods, "DELETE") {
		t.Errorf("expected the stale asset to be removed (DELETE), got %v", methods)
	}
}

func TestPublishRelease_EmptyTagErrors(t *testing.T) {
	p := &github.Provider{}

	err := p.PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if err == nil || !strings.Contains(err.Error(), "tag is empty") {
		t.Fatalf("err = %v, want tag-empty usage error", err)
	}
}

func TestUploadReleaseAsset_NewAssetUploadsDirectly(t *testing.T) {
	fake := newFake()
	fake.preloadRelease(false, false)

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	p := providerForFake(srv) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	dir := t.TempDir()

	asset := filepath.Join(dir, "binary.tgz")
	if err := os.WriteFile(asset, []byte("payload"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := p.UploadReleaseAsset(context.Background(), "v1.0.0", asset); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}

	methods := callMethods(fake.Calls())

	wantSeq := []string{"GET", "GET", "POST"} // tag lookup, list assets, upload
	if !equalPrefix(methods, wantSeq) {
		t.Errorf("call sequence = %v, want prefix %v", methods, wantSeq)
	}
}

func TestUploadReleaseAsset_ClobbersExistingAsset(t *testing.T) {
	fake := newFake()
	rel := fake.preloadRelease(false, false)
	rel.Assets = append(rel.Assets, &recordedAsset{ID: 555, Name: "binary.tgz"})

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	p := providerForFake(srv) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	dir := t.TempDir()

	asset := filepath.Join(dir, "binary.tgz")
	if err := os.WriteFile(asset, []byte("new"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	if err := p.UploadReleaseAsset(context.Background(), "v1.0.0", asset); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}

	methods := callMethods(fake.Calls())
	if !contains(methods, "DELETE") {
		t.Errorf("expected DELETE of existing asset, got %v", methods)
	}
}

func TestUploadReleaseAsset_MissingReleaseSurfaces404Class(t *testing.T) {
	fake := newFake()

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	p := providerForFake(srv) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	dir := t.TempDir()

	asset := filepath.Join(dir, "x.tgz")
	if err := os.WriteFile(asset, []byte("x"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	err := p.UploadReleaseAsset(context.Background(), "v9.9.9", asset)
	if err == nil || !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput from typed 404 mapping", err)
	}
}
