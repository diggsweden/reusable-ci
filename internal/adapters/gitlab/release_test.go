// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitlabserver"
)

// escaped project path GitLab uses in REST routes (url.PathEscape("itiquette/repo")).
const (
	glProject      = "itiquette%2Frepo"
	glReleasesBase = "/api/v4/projects/" + glProject + "/releases"
	glTag          = "v1.0.0"
)

func hasCall(reqs []fakegitlabserver.Request, method, path string) bool {
	for _, r := range reqs {
		if r.Method == method && r.Path == path {
			return true
		}
	}

	return false
}

func TestPublishRelease_CreatesWhenMissing(t *testing.T) {
	srv := fakegitlabserver.New(t)

	// The release does not exist yet.
	srv.OnGet(glReleasesBase+"/"+glTag, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 404, Body: `{"message":"404 Not Found"}`}
	})
	srv.OnPost(glReleasesBase, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{"tag_name":"v1.0.0"}`}
	})
	srv.OnGet(glReleasesBase+"/"+glTag+"/assets/links", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `[]`}
	})

	p := &gitlab.Provider{
		APIBaseOverride: srv.URL(),
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "t"}),
	}

	if err := p.PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:  glTag,
		Name: "v1.0.0",
	}); err != nil {
		t.Fatalf("PublishRelease: %v", err)
	}

	reqs := srv.Requests()
	if !hasCall(reqs, "POST", glReleasesBase) {
		t.Errorf("expected POST create, got %v", reqs)
	}

	for _, r := range reqs {
		if r.Method == http.MethodPut {
			t.Errorf("must not PUT-update a release that does not exist: %+v", r)
		}

		if r.Method == http.MethodDelete && r.Path == glReleasesBase+"/"+glTag {
			t.Errorf("must not delete the release object: %+v", r)
		}
	}
}

func TestPublishRelease_UpdatesInPlaceAndReconcilesAssets(t *testing.T) {
	srv := fakegitlabserver.New(t)

	// The release already exists.
	srv.OnGet(glReleasesBase+"/"+glTag, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `{"tag_name":"v1.0.0","name":"old"}`}
	})
	srv.OnPut(glReleasesBase+"/"+glTag, func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `{"tag_name":"v1.0.0","name":"renamed"}`}
	})
	// One pre-existing link that is no longer desired.
	srv.OnGet(glReleasesBase+"/"+glTag+"/assets/links", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `[{"id":1,"name":"stale.tgz"}]`}
	})
	srv.OnPost("/api/v4/projects/"+glProject+"/uploads", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{"url":"/uploads/abc/new.tgz","full_path":"/-/project/1/uploads/abc/new.tgz"}`}
	})
	srv.OnPost(glReleasesBase+"/"+glTag+"/assets/links", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{"id":2,"name":"new.tgz"}`}
	})
	srv.On("DELETE", glReleasesBase+"/"+glTag+"/assets/links/1", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 204}
	})

	dir := t.TempDir()

	asset := filepath.Join(dir, "new.tgz")
	if err := os.WriteFile(asset, []byte("payload"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	p := &gitlab.Provider{
		APIBaseOverride: srv.URL(),
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "t"}),
	}

	if err := p.PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:    glTag,
		Name:   "renamed",
		Assets: []string{asset},
	}); err != nil {
		t.Fatalf("PublishRelease: %v", err)
	}

	reqs := srv.Requests()

	if !hasCall(reqs, "PUT", glReleasesBase+"/"+glTag) {
		t.Errorf("expected in-place PUT update, got %v", reqs)
	}

	if !hasCall(reqs, "POST", "/api/v4/projects/"+glProject+"/uploads") {
		t.Errorf("expected asset upload POST, got %v", reqs)
	}

	if !hasCall(reqs, "POST", glReleasesBase+"/"+glTag+"/assets/links") {
		t.Errorf("expected asset link create, got %v", reqs)
	}

	if !hasCall(reqs, "DELETE", glReleasesBase+"/"+glTag+"/assets/links/1") {
		t.Errorf("expected stale asset link deletion, got %v", reqs)
	}

	for _, r := range reqs {
		if r.Method == http.MethodDelete && r.Path == glReleasesBase+"/"+glTag {
			t.Errorf("reconcile must not delete the release object: %+v", r)
		}
	}
}

func TestPublishRelease_EmptyTagErrors(t *testing.T) {
	p := &gitlab.Provider{}

	err := p.PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{})
	if err == nil {
		t.Fatal("expected empty-tag error")
	}
}
