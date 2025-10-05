// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"context"
	"crypto/sha256"
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
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

const (
	selfRuntimeTag       = domainrelease.SelfRuntimeChannelTag
	selfRuntimeSourceRef = "refs/heads/main"
)

type selfRuntimeAsset struct {
	id   int64
	name string
	body string
}

type selfRuntimeAPI struct {
	target           string
	sourceSHA        string
	tagSHA           string
	staleAfter       int
	sourceReads      int
	release          bool
	prerelease       bool
	releaseID        int64
	nextAssetID      int64
	assets           []*selfRuntimeAsset
	mutations        []string
	releaseDraft     bool
	failUploadName   string
	failTagMove      bool
	omitUploadDigest bool
	lastFresh        bool
	unfreshMutations []string
}

func newSelfRuntimeAPI(target string) *selfRuntimeAPI {
	return &selfRuntimeAPI{
		target:      target,
		sourceSHA:   target,
		releaseID:   7,
		nextAssetID: 100,
	}
}

//nolint:gocognit,gocyclo,cyclop,maintidx // one linear in-memory fake for the complete GitHub publication API.
func (a *selfRuntimeAPI) handler(t *testing.T) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const root = "/repos/diggsweden/reusable-ci"

		path := strings.TrimPrefix(r.URL.Path, root)
		freshRead := path == "/git/ref/heads/main" && r.Method == http.MethodGet

		mutation := r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if !freshRead {
			if mutation && !a.lastFresh {
				a.unfreshMutations = append(a.unfreshMutations, r.Method+" "+path)
			}

			a.lastFresh = false
		}

		switch {
		case path == "/git/ref/heads/main" && r.Method == http.MethodGet:
			a.sourceReads++

			sha := a.sourceSHA
			if a.staleAfter > 0 && a.sourceReads > a.staleAfter {
				sha = strings.Repeat("b", 40)
			}

			a.lastFresh = true

			writeSelfRuntimeRef(w, selfRuntimeSourceRef, sha)

		case path == "/releases/tags/"+selfRuntimeTag && r.Method == http.MethodGet:
			if !a.release {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

				return
			}

			writeSelfRuntimeRelease(w, a)

		case path == "/releases" && r.Method == http.MethodPost:
			var in struct {
				Draft           bool   `json:"draft"`
				Prerelease      bool   `json:"prerelease"`
				TargetCommitish string `json:"target_commitish"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)
			a.release = true
			a.prerelease = in.Prerelease
			a.releaseDraft = in.Draft
			a.tagSHA = in.TargetCommitish
			a.mutations = append(a.mutations, "create-draft")
			writeSelfRuntimeRelease(w, a)

		case path == "/releases/7" && r.Method == http.MethodPatch:
			var in struct {
				Draft      bool `json:"draft"`
				Prerelease bool `json:"prerelease"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)
			a.releaseDraft = in.Draft
			a.prerelease = in.Prerelease
			a.mutations = append(a.mutations, "publish-release")
			writeSelfRuntimeRelease(w, a)

		case path == "/releases/7/assets" && r.Method == http.MethodGet:
			out := make([]map[string]any, 0, len(a.assets))
			for _, asset := range a.assets {
				out = append(out, selfRuntimeAssetWire(asset))
			}

			_ = json.NewEncoder(w).Encode(out)

		case path == "/releases/7/assets" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)

			name := r.URL.Query().Get("name")
			if name == a.failUploadName {
				a.mutations = append(a.mutations, "upload-failed:"+name)

				http.Error(w, `{"message":"upload failed"}`, http.StatusInternalServerError)

				return
			}

			a.nextAssetID++
			asset := &selfRuntimeAsset{id: a.nextAssetID, name: name, body: string(body)}
			a.assets = append(a.assets, asset)
			a.mutations = append(a.mutations, "upload:"+asset.name)

			w.WriteHeader(http.StatusCreated)

			wire := selfRuntimeAssetWire(asset)
			if a.omitUploadDigest {
				delete(wire, "digest")
			}

			_ = json.NewEncoder(w).Encode(wire)

		case strings.HasPrefix(path, "/releases/assets/") && r.Method == http.MethodPatch:
			id := selfRuntimeAssetID(path)

			var in struct {
				Name string `json:"name"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)

			asset := a.asset(id)
			if asset == nil {
				http.Error(w, "not found", http.StatusNotFound)

				return
			}

			asset.name = in.Name
			a.mutations = append(a.mutations, "rename:"+in.Name)
			_ = json.NewEncoder(w).Encode(selfRuntimeAssetWire(asset))

		case strings.HasPrefix(path, "/releases/assets/") && r.Method == http.MethodGet:
			asset := a.asset(selfRuntimeAssetID(path))
			if asset == nil {
				http.Error(w, "not found", http.StatusNotFound)

				return
			}

			_, _ = w.Write([]byte(asset.body))

		case strings.HasPrefix(path, "/releases/assets/") && r.Method == http.MethodDelete:
			id := selfRuntimeAssetID(path)
			deletedName := "unknown"

			for i, asset := range a.assets {
				if asset.id == id {
					deletedName = asset.name

					a.assets = append(a.assets[:i], a.assets[i+1:]...)

					break
				}
			}

			a.mutations = append(a.mutations, "delete:"+deletedName)

			w.WriteHeader(http.StatusNoContent)

		case path == "/git/ref/tags/"+selfRuntimeTag && r.Method == http.MethodGet:
			if a.tagSHA == "" {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

				return
			}

			writeSelfRuntimeRef(w, "refs/tags/"+selfRuntimeTag, a.tagSHA)

		case path == "/git/refs/tags/"+selfRuntimeTag && r.Method == http.MethodPatch:
			if a.failTagMove {
				a.mutations = append(a.mutations, "move-tag-failed")

				http.Error(w, `{"message":"tag move failed"}`, http.StatusInternalServerError)

				return
			}

			var in struct {
				SHA string `json:"sha"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)
			a.tagSHA = in.SHA
			a.mutations = append(a.mutations, "move-tag")
			writeSelfRuntimeRef(w, "refs/tags/"+selfRuntimeTag, a.tagSHA)

		case path == "/git/refs" && r.Method == http.MethodPost:
			var in struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			}

			_ = json.NewDecoder(r.Body).Decode(&in)
			a.tagSHA = in.SHA
			a.mutations = append(a.mutations, "move-tag")

			writeSelfRuntimeRef(w, in.Ref, in.SHA)

		default:
			http.Error(w, fmt.Sprintf("unhandled %s %s", r.Method, r.URL.Path), http.StatusNotFound)
		}
	})
}

func (a *selfRuntimeAPI) asset(id int64) *selfRuntimeAsset {
	for _, asset := range a.assets {
		if asset.id == id {
			return asset
		}
	}

	return nil
}

func selfRuntimeAssetID(path string) int64 {
	var id int64

	_, _ = fmt.Sscanf(strings.TrimPrefix(path, "/releases/assets/"), "%d", &id)

	return id
}

func selfRuntimeAssetWire(asset *selfRuntimeAsset) map[string]any {
	digest := sha256.Sum256([]byte(asset.body))

	return map[string]any{
		"id":     asset.id,
		"name":   asset.name,
		"size":   len(asset.body),
		"digest": fmt.Sprintf("sha256:%x", digest),
	}
}

func writeSelfRuntimeRelease(w http.ResponseWriter, api *selfRuntimeAPI) {
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errchkjson // test fixture contains only JSON-safe scalar values.
		"id":         api.releaseID,
		"tag_name":   selfRuntimeTag,
		"draft":      api.releaseDraft,
		"prerelease": api.prerelease,
	})
}

func writeSelfRuntimeRef(w http.ResponseWriter, ref, sha string) {
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errchkjson // test fixture contains only JSON-safe scalar values.
		"ref": ref, "object": map[string]string{"type": "commit", "sha": sha},
	})
}

func selfRuntimePublisher(t *testing.T, api *selfRuntimeAPI) *github.Provider {
	t.Helper()
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)

	return &github.Provider{HTTPClient: srv.Client(), APIBaseOverride: srv.URL}
}

func writeSelfRuntimeTestAssets(t *testing.T, bodies map[string]string) []string {
	t.Helper()
	dir := t.TempDir()

	paths := make([]string, 0, len(bodies))
	for name, body := range bodies {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		paths = append(paths, path)
	}

	return paths
}

func TestPublishSelfRuntimeCLI_ReconcilesInPlaceChecksumLastAndTagLast(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, strings.Repeat("c", 40)
	api.assets = []*selfRuntimeAsset{
		{id: 1, name: "binary.tgz", body: "old-binary"},
		{id: 2, name: "checksums.txt.bundle", body: "old-bundle"},
		{id: 3, name: "checksums.txt", body: "old-checksum"},
		{id: 4, name: "stale.txt", body: "stale"},
	}

	assets := writeSelfRuntimeTestAssets(t, map[string]string{
		"binary.tgz":           "new-binary",
		"checksums.txt.bundle": "new-bundle",
		"checksums.txt":        "new-checksum",
	})

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body",
		provider.ReleaseSpec{Tag: selfRuntimeTag, Prerelease: true, MakeLatest: provider.MakeLatestFalse, Assets: assets},
	)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, asset := range api.assets {
		got[asset.name] = asset.body
	}

	for name, body := range map[string]string{
		"binary.tgz": "new-binary", "checksums.txt.bundle": "new-bundle", "checksums.txt": "new-checksum",
	} {
		if got[name] != body {
			t.Errorf("asset %s = %q, want %q (all: %v)", name, got[name], body, got)
		}
	}

	if len(got) != 3 {
		t.Errorf("final asset set = %v, want exactly three desired assets", got)
	}

	if slices.Contains(api.mutations, "delete-release") {
		t.Fatalf("release was deleted: %v", api.mutations)
	}

	uploads := make([]string, 0, 3)

	for _, mutation := range api.mutations {
		if strings.HasPrefix(mutation, "upload:") {
			uploads = append(uploads, mutation)
		}
	}

	if len(uploads) != 3 || !strings.Contains(uploads[0], "binary.tgz") || !strings.Contains(uploads[1], "checksums.txt.bundle") || !strings.Contains(uploads[2], "checksums.txt") {
		t.Errorf("upload order = %v, want payload, bundle, checksum", uploads)
	}

	if api.mutations[len(api.mutations)-1] != "move-tag" || api.tagSHA != target {
		t.Errorf("mutations = %v, tag = %s; want tag move last to target", api.mutations, api.tagSHA)
	}

	for _, mutation := range api.mutations {
		if strings.HasPrefix(mutation, "rename:") || strings.Contains(mutation, ".reusable-ci-") {
			t.Errorf("rolling publication used staged asset renaming: %v", api.mutations)
		}
	}

	if len(api.unfreshMutations) != 0 {
		t.Errorf("mutations without an immediately preceding freshness check: %v", api.unfreshMutations)
	}
}

func TestPublishSelfRuntimeCLI_StaleSourceFailsBeforeMutation(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, strings.Repeat("c", 40)
	api.sourceSHA = strings.Repeat("b", 40)

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want stale-source validation", err)
	}

	if len(api.mutations) != 0 {
		t.Fatalf("stale source mutated release: %v", api.mutations)
	}
}

func TestPublishSelfRuntimeCLI_RequiresProvisionedPrerelease(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "must be provisioned") {
		t.Fatalf("err = %v, want provisioned-prerelease validation", err)
	}

	if len(api.mutations) != 0 || api.tagSHA != "" {
		t.Fatalf("missing release mutated channel: tag=%s mutations=%v", api.tagSHA, api.mutations)
	}
}

func TestPublishSelfRuntimeCLI_SourceAdvancedBeforeTagMove(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	oldTag := strings.Repeat("c", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, oldTag
	api.staleAfter = 8
	api.assets = []*selfRuntimeAsset{
		{id: 1, name: "binary.tgz", body: "old-binary"},
		{id: 2, name: "checksums.txt.bundle", body: "old-bundle"},
		{id: 3, name: "checksums.txt", body: "old-checksum"},
		{id: 4, name: "stale.txt", body: "stale"},
	}

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want stale-source validation", err)
	}

	if api.tagSHA != oldTag || slices.Contains(api.mutations, "move-tag") {
		t.Fatalf("stale run moved tag: tag=%s mutations=%v", api.tagSHA, api.mutations)
	}

	assertSelfRuntimeAssetBodies(t, api.assets, map[string]string{
		"binary.tgz": "old-binary", "checksums.txt.bundle": "old-bundle", "checksums.txt": "old-checksum", "stale.txt": "stale",
	})
}

func TestPublishSelfRuntimeCLI_TagMoveFailureRestoresPriorAssets(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	oldTag := strings.Repeat("c", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, oldTag
	api.failTagMove = true
	api.assets = []*selfRuntimeAsset{
		{id: 1, name: "binary.tgz", body: "old-binary"},
		{id: 2, name: "checksums.txt.bundle", body: "old-bundle"},
		{id: 3, name: "checksums.txt", body: "old-checksum"},
	}

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrDependencyUnavailable) || !strings.Contains(err.Error(), "move self-runtime tag") {
		t.Fatalf("tag move error = %v, want ErrDependencyUnavailable naming the tag move", err)
	}

	if api.tagSHA != oldTag {
		t.Fatalf("failed tag move changed tag to %s", api.tagSHA)
	}

	assertSelfRuntimeAssetBodies(t, api.assets, map[string]string{
		"binary.tgz": "old-binary", "checksums.txt.bundle": "old-bundle", "checksums.txt": "old-checksum",
	})
}

func TestPublishSelfRuntimeCLI_RefusesNonPrereleaseOrUntrustedInput(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.tagSHA = true, target

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "non-prerelease") {
		t.Fatalf("err = %v, want non-prerelease refusal", err)
	}

	err = selfRuntimePublisher(t, newSelfRuntimeAPI(target)).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, "refs/heads/untrusted", target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("untrusted source err = %v, want validation", err)
	}
}

func TestPublishSelfRuntimeCLI_PartialFailureLeavesOldChecksumFailClosed(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	oldTag := strings.Repeat("c", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, oldTag
	api.failUploadName = "checksums.txt.bundle"
	api.assets = []*selfRuntimeAsset{
		{id: 1, name: "binary.tgz", body: "old-binary"},
		{id: 2, name: "checksums.txt.bundle", body: "old-bundle"},
		{id: 3, name: "checksums.txt", body: "old-checksum"},
	}

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("bundle upload error = %v, want the forge's 500 classified as ErrDependencyUnavailable", err)
	}

	got := map[string]string{}
	for _, asset := range api.assets {
		got[asset.name] = asset.body
	}

	if got["binary.tgz"] != "new-binary" {
		t.Errorf("payload was not replaced before failure: %v", got)
	}

	if _, exists := got["checksums.txt.bundle"]; exists {
		t.Errorf("failed bundle replacement left a bundle: %v", got)
	}

	if got["checksums.txt"] != "old-checksum" {
		t.Errorf("checksum commit point changed after pre-commit failure: %v", got)
	}

	if api.tagSHA != oldTag || slices.Contains(api.mutations, "move-tag") {
		t.Fatalf("partial publication moved channel tag: tag=%s mutations=%v", api.tagSHA, api.mutations)
	}

	if slices.Contains(api.mutations, "delete:checksums.txt") {
		t.Fatalf("checksum was mutated before bundle succeeded: %v", api.mutations)
	}
}

func TestPublishSelfRuntimeCLI_DigestFailureCleanupChecksFreshness(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	oldTag := strings.Repeat("c", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, oldTag
	api.omitUploadDigest = true

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", validSelfRuntimeSpec(t),
	)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("missing upload digest error = %v, want ErrValidation", err)
	}

	if len(api.assets) != 0 {
		t.Fatalf("unverified upload was not removed: %+v", api.assets)
	}

	if len(api.unfreshMutations) != 0 {
		t.Fatalf("digest cleanup mutated without a fresh source check: %v", api.unfreshMutations)
	}

	if api.tagSHA != oldTag || slices.Contains(api.mutations, "move-tag") {
		t.Fatalf("failed upload moved channel tag: tag=%s mutations=%v", api.tagSHA, api.mutations)
	}
}

func TestPublishSelfRuntimeCLI_RequiresSignedChecksumCommitAssets(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)
	spec := provider.ReleaseSpec{
		Tag: selfRuntimeTag, Prerelease: true, MakeLatest: provider.MakeLatestFalse,
		Assets: writeSelfRuntimeTestAssets(t, map[string]string{"binary.tgz": "binary", "checksums.txt": "checksum"}),
	}

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body", spec,
	)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "checksums.txt.bundle") {
		t.Fatalf("missing signed checksum bundle error = %v", err)
	}

	if len(api.mutations) != 0 {
		t.Fatalf("invalid asset set caused mutations: %v", api.mutations)
	}
}

func validSelfRuntimeSpec(t *testing.T) provider.ReleaseSpec {
	t.Helper()

	return provider.ReleaseSpec{
		Tag: selfRuntimeTag, Prerelease: true, MakeLatest: provider.MakeLatestFalse,
		Assets: writeSelfRuntimeTestAssets(t, map[string]string{
			"binary.tgz": "new-binary", "checksums.txt.bundle": "new-bundle", "checksums.txt": "new-checksum",
		}),
	}
}

func assertSelfRuntimeAssetBodies(t *testing.T, assets []*selfRuntimeAsset, want map[string]string) {
	t.Helper()

	got := make(map[string]string, len(assets))
	for _, asset := range assets {
		got[asset.name] = asset.body
	}

	if len(got) != len(want) {
		t.Fatalf("asset set = %v, want %v", got, want)
	}

	for name, body := range want {
		if got[name] != body {
			t.Errorf("asset %q = %q, want %q (all: %v)", name, got[name], body, got)
		}
	}
}

// TestPublishSelfRuntimeCLI_RefusesHostileAssetNames exercises the guard that
// decides where a snapshotted asset is written.
//
// Before replacing the self-runtime release's assets, the publisher downloads
// the current ones into a temporary directory so it can put them back if the
// publication fails. Each file is placed at filepath.Join(dir, name), where
// name comes from the GitHub API — so the guard on that name is what keeps the
// snapshot inside the directory it owns. Nothing exercised it: the untrusted-
// input test covers the source ref and the prerelease flag, not the names.
//
// ".." is the case worth naming. It is not caught by "contains a separator",
// because filepath.Base("..") is "..", and it is not the empty or "." case
// either — it is a single path component that happens to mean "the parent".
func TestPublishSelfRuntimeCLI_RefusesHostileAssetNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"..",
		".",
		"",
		"../escape.txt",
		"nested/asset.txt",
		"/absolute.txt",
		"a/../../escape.txt",
	} {
		t.Run("name="+name, func(t *testing.T) {
			t.Parallel()

			target := strings.Repeat("a", 40)
			api := newSelfRuntimeAPI(target)
			api.release, api.prerelease, api.tagSHA = true, true, strings.Repeat("c", 40)
			api.assets = []*selfRuntimeAsset{{id: 1, name: name, body: "hostile"}}

			assets := writeSelfRuntimeTestAssets(t, map[string]string{
				"binary.tgz":           "new-binary",
				"checksums.txt.bundle": "new-bundle",
				"checksums.txt":        "new-checksum",
			})

			err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
				context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body",
				provider.ReleaseSpec{Tag: selfRuntimeTag, Prerelease: true, MakeLatest: provider.MakeLatestFalse, Assets: assets},
			)
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("asset name %q: err = %v, want ErrValidation", name, err)
			}

			if !strings.Contains(err.Error(), "unsafe asset name") {
				t.Errorf("asset name %q: err = %v, want the unsafe-name diagnostic", name, err)
			}
		})
	}
}

// TestPublishSelfRuntimeCLI_RefusesDuplicateAssetNames is the neighbouring
// guard: two assets with one name would have the second overwrite the first in
// the snapshot directory, so a restore would put the same bytes back twice and
// silently lose one of the originals.
func TestPublishSelfRuntimeCLI_RefusesDuplicateAssetNames(t *testing.T) {
	t.Parallel()

	target := strings.Repeat("a", 40)
	api := newSelfRuntimeAPI(target)
	api.release, api.prerelease, api.tagSHA = true, true, strings.Repeat("c", 40)
	api.assets = []*selfRuntimeAsset{
		{id: 1, name: "binary.tgz", body: "first"},
		{id: 2, name: "binary.tgz", body: "second"},
	}

	assets := writeSelfRuntimeTestAssets(t, map[string]string{
		"binary.tgz":           "new-binary",
		"checksums.txt.bundle": "new-bundle",
		"checksums.txt":        "new-checksum",
	})

	err := selfRuntimePublisher(t, api).PublishSelfRuntimeCLI(
		context.Background(), domainrelease.SelfRuntimeRepository, selfRuntimeSourceRef, target, "body",
		provider.ReleaseSpec{Tag: selfRuntimeTag, Prerelease: true, MakeLatest: provider.MakeLatestFalse, Assets: assets},
	)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "duplicate asset") {
		t.Fatalf("err = %v, want the duplicate-asset refusal", err)
	}
}
