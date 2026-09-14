// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	mu                            sync.Mutex
	calls                         []recordedCall
	releases                      map[string]*recordedRelease
	nextID                        int64
	failUploadName                string
	failUploadRemaining           int
	failPatchAfterCommitRemaining int
	failPatchRemaining            int
	omitUploadDigest              bool
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

func (f *fakeGitHub) releaseSnapshot(tag string) *recordedRelease {
	f.mu.Lock()
	defer f.mu.Unlock()

	rel := f.releases[tag]
	if rel == nil {
		return nil
	}

	snapshot := *rel
	snapshot.Assets = append([]*recordedAsset(nil), rel.Assets...)

	return &snapshot
}

//nolint:cyclop,gocognit,maintidx // test fixture mux dispatches every method/path the GitHub adapter touches — one flat route table, deliberately in one place so the fake reads as the API surface it stands in for.
func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases/tags/", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))
		tag := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/releases/tags/")

		f.mu.Lock()
		rel := f.releases[tag]
		f.mu.Unlock()

		if rel == nil || rel.Draft {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

			return
		}

		writeRelease(w, rel)
	})
	mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		body, _ := io.ReadAll(r.Body)
		f.record(r, string(body))

		if r.Method == http.MethodGet {
			f.mu.Lock()

			releases := make([]map[string]any, 0, len(f.releases))
			for _, rel := range f.releases {
				releases = append(releases, releaseToWire(rel))
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(releases)

			return
		}

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
		case strings.HasPrefix(path, "assets/") && r.Method == http.MethodDelete:
			var id int64

			_, _ = fmt.Sscanf(strings.TrimPrefix(path, "assets/"), "%d", &id)

			f.mu.Lock()

			for _, rel := range f.releases {
				rel.Assets = filterOutAsset(rel.Assets, id)
			}

			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(path, "assets/") && r.Method == http.MethodPatch:
			var id int64

			_, _ = fmt.Sscanf(strings.TrimPrefix(path, "assets/"), "%d", &id)

			var in struct {
				Name string `json:"name"`
			}

			_ = json.Unmarshal(body, &in)

			f.mu.Lock()
			for _, rel := range f.releases {
				for _, asset := range rel.Assets {
					if asset.ID == id {
						asset.Name = in.Name
						f.mu.Unlock()

						_ = json.NewEncoder(w).Encode(assetToWire(asset))

						return
					}
				}
			}
			f.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
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

			// A PATCH the server never applied: the release stays as it was.
			if f.failPatchRemaining > 0 {
				f.failPatchRemaining--
				f.mu.Unlock()
				http.Error(w, "bad gateway", http.StatusBadGateway)

				return
			}

			for _, rel := range f.releases {
				if rel.ID == id {
					rel.TagName, rel.Name = in.TagName, in.Name
					rel.Draft, rel.Prerelease = in.Draft, in.Prerelease
					rel.MakeLatest, rel.Body = in.MakeLatest, in.Body

					failAfterCommit := f.failPatchAfterCommitRemaining > 0
					if failAfterCommit {
						f.failPatchAfterCommitRemaining--
					}

					f.mu.Unlock()

					if failAfterCommit {
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{"id":`))

						return
					}

					writeRelease(w, rel)

					return
				}
			}

			f.mu.Unlock()
			http.Error(w, "not found", http.StatusNotFound)
		case r.Method == http.MethodGet:
			id := releaseIDFromPath(path)

			f.mu.Lock()

			var found *recordedRelease

			for _, rel := range f.releases {
				if rel.ID == id {
					found = rel

					break
				}
			}
			f.mu.Unlock()

			if found == nil {
				http.Error(w, "not found", http.StatusNotFound)

				return
			}

			writeRelease(w, found)
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
		if (name == f.failUploadName || strings.HasPrefix(name, f.failUploadName+".reusable-ci-upload-")) && f.failUploadRemaining > 0 {
			f.failUploadRemaining--
			f.mu.Unlock()
			http.Error(w, "injected upload failure", http.StatusInternalServerError)

			return
		}

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

		wire := assetToWire(a)
		if f.omitUploadDigest {
			delete(wire, "digest")
		}

		_ = json.NewEncoder(w).Encode(wire)
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

	if err := json.NewEncoder(w).Encode(releaseToWire(rel)); err != nil {
		panic(err) // test fixture; payload is fully typed-safe
	}
}

func releaseToWire(rel *recordedRelease) map[string]any {
	return map[string]any{
		"id":          rel.ID,
		"tag_name":    rel.TagName,
		"name":        rel.Name,
		"draft":       rel.Draft,
		"prerelease":  rel.Prerelease,
		"make_latest": rel.MakeLatest,
		"body":        rel.Body,
	}
}

func assetsToWire(assets []*recordedAsset) []map[string]any {
	out := make([]map[string]any, 0, len(assets))
	for _, a := range assets {
		out = append(out, assetToWire(a))
	}

	return out
}

func assetToWire(asset *recordedAsset) map[string]any {
	digest := sha256.Sum256([]byte(asset.Body))

	return map[string]any{
		"id":     asset.ID,
		"name":   asset.Name,
		"size":   len(asset.Body),
		"digest": "sha256:" + hex.EncodeToString(digest[:]),
	}
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

func TestCreateRelease_NoExistingRelease(t *testing.T) {
	t.Parallel()

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
	if len(calls) < 3 {
		t.Fatalf("calls = %v, want list + draft create + publish", calls)
	}

	if calls[0].Method != http.MethodGet || calls[0].Path != "/repos/owner/repo/releases" {
		t.Errorf("first call = %+v, want GET releases", calls[0])
	}

	if calls[1].Method != http.MethodPost || calls[1].Path != "/repos/owner/repo/releases" {
		t.Errorf("second call = %+v, want POST releases", calls[1])
	}

	if !strings.Contains(calls[1].Body, `"draft":true`) || !strings.Contains(calls[1].Body, `"make_latest":"false"`) {
		t.Errorf("draft create body = %s, want private non-latest staging", calls[1].Body)
	}

	var publish *recordedCall

	for i := range calls {
		if calls[i].Method == http.MethodPatch {
			publish = &calls[i]
		}
	}

	if publish == nil || !strings.Contains(publish.Body, `"draft":false`) ||
		!strings.Contains(publish.Body, `"make_latest":"true"`) {
		t.Errorf("publish call = %+v, want requested visible/latest state", publish)
	}

	for _, c := range calls {
		if c.Method == http.MethodDelete {
			t.Errorf("did not expect DELETE on no-existing-release path: %+v", c)
		}
	}

	if got := fake.releaseSnapshot("v1.0.0"); got == nil || got.Draft {
		t.Fatalf("final release = %+v, want visible release", got)
	}
}

func TestCreateRelease_PublishesOnlyAfterEveryAssetAndRetryRecovers(t *testing.T) {
	t.Parallel()

	fake := newFake()
	fake.failUploadName = "second.tgz"
	fake.failUploadRemaining = 1

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	dir := t.TempDir()
	first := filepath.Join(dir, "first.tgz")

	second := filepath.Join(dir, "second.tgz")
	for path, content := range map[string]string{first: "first", second: "second"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
	}

	p := providerForFake(srv)

	spec := provider.ReleaseSpec{
		Tag:        "v1.0.0",
		Name:       "v1.0.0",
		MakeLatest: provider.MakeLatestTrue,
		Assets:     []string{first, second},
	}
	if err := p.CreateRelease(context.Background(), "owner/repo", spec); err == nil {
		t.Fatal("CreateRelease succeeded despite injected second-asset failure")
	}

	failedCalls := fake.Calls()
	for _, call := range failedCalls {
		if call.Method == http.MethodPatch {
			t.Fatalf("release became visible before every asset uploaded: %+v", failedCalls)
		}
	}

	failed := fake.releaseSnapshot(spec.Tag)
	if failed == nil || !failed.Draft {
		t.Fatalf("failed release = %+v, want retained draft", failed)
	}

	if len(failed.Assets) != 1 || failed.Assets[0].Name != filepath.Base(first) {
		t.Fatalf("failed release assets = %+v, want only first asset", failed.Assets)
	}

	retryStart := len(failedCalls)

	if err := p.CreateRelease(context.Background(), "owner/repo", spec); err != nil {
		t.Fatalf("retry CreateRelease: %v", err)
	}

	retryCalls := fake.Calls()[retryStart:]
	if methods := callMethods(retryCalls); !equalPrefix(methods, []string{"GET", "DELETE", "POST"}) {
		t.Fatalf("retry methods = %v, want draft replacement prefix", methods)
	}

	patchIndex := -1
	lastUploadIndex := -1

	for i, call := range retryCalls {
		if call.Method == http.MethodPost && strings.HasSuffix(call.Path, "/assets") {
			lastUploadIndex = i
		}

		if call.Method == http.MethodPatch {
			patchIndex = i
		}
	}

	if lastUploadIndex < 0 || patchIndex <= lastUploadIndex {
		t.Fatalf("retry calls = %+v, want publication PATCH after all uploads", retryCalls)
	}

	final := fake.releaseSnapshot(spec.Tag)
	if final == nil || final.Draft {
		t.Fatalf("final release = %+v, want visible release", final)
	}

	if len(final.Assets) != 2 {
		t.Fatalf("final release assets = %+v, want both assets", final.Assets)
	}
}

func TestCreateRelease_RecoversCommittedPublishWithLostResponse(t *testing.T) {
	t.Parallel()

	fake := newFake()
	fake.failPatchAfterCommitRemaining = 1

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)
	if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0",
		Name:       "v1.0.0",
		MakeLatest: provider.MakeLatestTrue,
	}); err != nil {
		t.Fatalf("CreateRelease after committed PATCH response loss: %v", err)
	}

	final := fake.releaseSnapshot("v1.0.0")
	if final == nil || final.Draft {
		t.Fatalf("final release = %+v, want reconciled visible release", final)
	}

	methods := callMethods(fake.Calls())
	if !equalPrefix(methods, []string{"GET", "POST", "PATCH", "GET"}) {
		t.Fatalf("methods = %v, want list, draft create, ambiguous publish, state reconciliation", methods)
	}
}

func TestCreateRelease_StagesPrereleaseAsDraft(t *testing.T) {
	t.Parallel()

	fake := newFake()

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)
	if err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v2.0.0-rc.1",
		Name:       "v2.0.0-rc.1",
		Prerelease: true,
		MakeLatest: provider.MakeLatestFalse,
	}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	var create, publish *recordedCall

	calls := fake.Calls()
	for i := range calls {
		call := &calls[i]
		if call.Method == http.MethodPost {
			create = call
		}

		if call.Method == http.MethodPatch {
			publish = call
		}
	}

	if create == nil || !strings.Contains(create.Body, `"draft":true`) ||
		!strings.Contains(create.Body, `"prerelease":true`) {
		t.Fatalf("prerelease create = %+v, want draft prerelease", create)
	}

	if publish == nil || !strings.Contains(publish.Body, `"draft":false`) ||
		!strings.Contains(publish.Body, `"prerelease":true`) || !strings.Contains(publish.Body, `"make_latest":"false"`) {
		t.Fatalf("prerelease publish = %+v, want requested final prerelease", publish)
	}
}

func TestCreateRelease_DeletesExistingDraftOrPrerelease(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		draft      bool
		prerelease bool
	}{
		{name: "draft", draft: true},
		{name: "prerelease", prerelease: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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

			if methods := callMethods(fake.Calls()); !equalPrefix(methods, []string{"GET", "DELETE", "POST"}) {
				t.Fatalf("methods = %v, want list, delete, draft create", methods)
			}
		})
	}
}

func TestCreateRelease_RefusesExistingStable(t *testing.T) {
	t.Parallel()

	fake := newFake()
	fake.preloadRelease(false, false)

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	p := providerForFake(srv)

	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.0.0",
		MakeLatest: provider.MakeLatestTrue,
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("err = %v, want refusal", err)
	}

	for _, c := range fake.Calls() {
		if c.Method == http.MethodDelete {
			t.Errorf("must not delete a stable release: %+v", c)
		}
	}
}

func TestCreateRelease_ReusesExactlyMatchingStable(t *testing.T) {
	t.Parallel()

	fake := newFake()
	release := fake.preloadRelease(false, false)
	release.Name = "Release v1.0.0"
	release.Body = "notes\n"
	release.Assets = []*recordedAsset{{ID: 2001, Name: "app.tar.gz", Body: "artifact"}}

	dir := t.TempDir()

	asset := filepath.Join(dir, "app.tar.gz")
	if err := os.WriteFile(asset, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}

	notes := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(notes, []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	err := providerForFake(srv).CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:       "v1.0.0",
		Name:      "Release v1.0.0",
		NotesFile: notes,
		Assets:    []string{asset},
	})
	if err != nil {
		t.Fatalf("exact stable rerun failed: %v", err)
	}

	for _, call := range fake.Calls() {
		if call.Method != http.MethodGet {
			t.Fatalf("exact stable rerun mutated release with %+v", call)
		}
	}
}

func TestCreateRelease_PostsDraftPrereleaseAndMakeLatestFlags(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	p := &github.Provider{}

	err := p.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "tag is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateRelease_ForwardsNotesFileBody(t *testing.T) {
	t.Parallel()

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

	staged := false
	final := false

	for _, call := range fake.Calls() {
		if call.Method == http.MethodPost && strings.Contains(call.Body, `"body":"# Release notes\n\nfoo"`) {
			staged = true
		}

		if call.Method == http.MethodPatch && strings.Contains(call.Body, `"body":"# Release notes\n\nfoo"`) {
			final = true
		}
	}

	if !staged || !final {
		t.Fatalf("release notes were not carried through draft creation and publication: %+v", fake.Calls())
	}
}

func TestPublishRelease_CreatesWhenMissing(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

	if !slices.Contains(methods, "PATCH") {
		t.Errorf("expected in-place PATCH edit of the existing release, got %v", methods)
	}

	// The release object itself must never be deleted during reconcile.
	releasePath := fmt.Sprintf("/repos/owner/repo/releases/%d", rel.ID)
	for _, c := range calls {
		if c.Method == http.MethodDelete && c.Path == releasePath {
			t.Errorf("reconcile must not delete the release object: %+v", c)
		}
	}

	if !slices.Contains(methods, "POST") {
		t.Errorf("expected the desired asset to be uploaded (POST), got %v", methods)
	}

	if !slices.Contains(methods, "DELETE") {
		t.Errorf("expected the stale asset to be removed (DELETE), got %v", methods)
	}
}

func TestPublishRelease_EmptyTagErrors(t *testing.T) {
	t.Parallel()

	p := &github.Provider{}

	err := p.PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "tag is empty") {
		t.Fatalf("err = %v, want tag-empty usage error", err)
	}
}

func TestUploadReleaseAsset_NewAssetUploadsDirectly(t *testing.T) {
	t.Parallel()

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

func TestUploadReleaseAsset_RejectsUploadWithoutDigest(t *testing.T) {
	t.Parallel()

	fake := newFake()
	fake.preloadRelease(false, false)
	fake.omitUploadDigest = true

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	asset := filepath.Join(t.TempDir(), "binary.tgz")
	if err := os.WriteFile(asset, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := providerForFake(srv).UploadReleaseAsset(context.Background(), "v1.0.0", asset)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want digest validation failure", err)
	}

	if got := fake.releaseSnapshot("v1.0.0"); len(got.Assets) != 0 {
		t.Fatalf("unverified upload was not removed: %+v", got.Assets)
	}
}

func TestUploadReleaseAsset_ClobbersExistingAsset(t *testing.T) {
	t.Parallel()

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
	if !slices.Contains(methods, "DELETE") {
		t.Errorf("expected DELETE of existing asset, got %v", methods)
	}

	if got := fake.releaseSnapshot("v1.0.0"); len(got.Assets) != 1 || got.Assets[0].Name != "binary.tgz" || got.Assets[0].Body != "new" {
		t.Errorf("replacement asset = %+v, want one promoted new binary.tgz", got.Assets)
	}

	post, firstPatch, firstDelete := -1, -1, -1

	for i, call := range fake.Calls() {
		switch {
		case call.Method == http.MethodPost && strings.HasSuffix(call.Path, "/assets"):
			post = i
		case call.Method == http.MethodPatch && firstPatch < 0:
			firstPatch = i
		case call.Method == http.MethodDelete && firstDelete < 0:
			firstDelete = i
		}
	}

	if post < 0 || firstPatch < post || firstDelete < firstPatch {
		t.Errorf("replacement call order = %+v, want upload before rename before delete", fake.Calls())
	}
}

func TestUploadReleaseAsset_FailedReplacementKeepsExistingAsset(t *testing.T) {
	t.Parallel()

	fake := newFake()
	rel := fake.preloadRelease(false, false)
	rel.Assets = append(rel.Assets, &recordedAsset{ID: 555, Name: "binary.tgz", Body: "known-good"})
	fake.failUploadName = "binary.tgz"
	fake.failUploadRemaining = 1

	srv := httptest.NewServer(combinedHandler(fake))
	defer srv.Close()

	asset := filepath.Join(t.TempDir(), "binary.tgz")
	if err := os.WriteFile(asset, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := providerForFake(srv).UploadReleaseAsset(context.Background(), "v1.0.0", asset)
	if err == nil {
		t.Fatal("expected replacement upload failure")
	}

	got := fake.releaseSnapshot("v1.0.0")
	if len(got.Assets) != 1 || got.Assets[0].Name != "binary.tgz" || got.Assets[0].Body != "known-good" {
		t.Fatalf("failed replacement changed existing asset: %+v", got.Assets)
	}
}

func TestUploadReleaseAsset_MissingReleaseSurfaces404Class(t *testing.T) {
	t.Parallel()

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
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput from typed 404 mapping", err)
	}
}
