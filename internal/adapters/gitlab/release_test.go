// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
	t.Parallel()

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
		HTTPClient:      srv.Client(),
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
	t.Parallel()

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
		HTTPClient:      srv.Client(),
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
	t.Parallel()

	p := &gitlab.Provider{}

	err := p.PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "tag is empty") {
		t.Fatalf("err = %v, want ErrUsage naming the empty tag", err)
	}
}

func TestPublishRelease_AssetUploadRequiresGitLabTokenBeforeMutation(t *testing.T) {
	t.Parallel()

	asset := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := os.WriteFile(asset, []byte("owned asset"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	p := &gitlab.Provider{
		APIBaseOverride: "https://gitlab.invalid",
		Env:             envFunc(map[string]string{"CI_JOB_TOKEN": "ci-job-token"}),
		HTTPClient: &http.Client{Transport: releasePolicyTransport(func(*http.Request) (*http.Response, error) {
			calls++

			return nil, errs.ErrDependencyUnavailable
		})},
	}

	err := p.PublishRelease(context.Background(), "itiquette/repo", provider.ReleaseSpec{
		Tag:    glTag,
		Assets: []string{asset},
	})
	if !errors.Is(err, errs.ErrPermissionDenied) || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Fatalf("err = %v, want GITLAB_TOKEN permission error", err)
	}

	if calls != 0 {
		t.Fatalf("provider called before rejecting unsupported asset auth: %d", calls)
	}
}

// TestPublishRelease_UnreadableNotesFileFailsBeforeAnyRequest pins that a
// notes file that cannot be read fails the publish instead of shipping the
// release with its name as the body, matching GitHub and Forgejo.
func TestPublishRelease_UnreadableNotesFileFailsBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	srv := fakegitlabserver.New(t)

	p := &gitlab.Provider{
		APIBaseOverride: srv.URL(),
		HTTPClient:      srv.Client(),
		Env:             envFunc(map[string]string{"GITLAB_TOKEN": "t"}),
	}

	spec := provider.ReleaseSpec{Tag: glTag, Name: "v1.0.0", NotesFile: filepath.Join(t.TempDir(), "missing-notes.md")}

	for name, publish := range map[string]func() error{
		"PublishRelease": func() error { return p.PublishRelease(context.Background(), "itiquette/repo", spec) },
		"CreateRelease":  func() error { return p.CreateRelease(context.Background(), "itiquette/repo", spec) },
	} {
		err := publish()
		if err == nil || !strings.Contains(err.Error(), "release notes") {
			t.Errorf("%s = %v, want a read-notes failure", name, err)
		}
	}

	if reqs := srv.Requests(); len(reqs) != 0 {
		t.Errorf("requests were sent although the notes could not be read: %v", reqs)
	}
}
