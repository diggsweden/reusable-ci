// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// wireReply is one canned answer; next, when set, becomes a Link header
// advertising that page.
type wireReply struct {
	status int
	body   string
	next   string
}

// wireAPI answers "METHOD path?query" exactly and records every request with
// its Authorization header and body. An unlisted request fails the test;
// failOn answers that one request with failStatus instead of its reply.
type wireAPI struct {
	t          *testing.T
	replies    map[string]wireReply
	failOn     string
	failStatus int
	bodies     map[string]string

	mu    sync.Mutex
	trace []string
}

func (api *wireAPI) provider() *forgejo.Provider {
	return &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok", "FORGEJO_REPOSITORY": "owner/repo"}),
		APIBaseOverride: "https://forgejo.invalid",
		HTTPClient: &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
			key := req.Method + " " + req.URL.RequestURI()

			var body []byte
			if req.Body != nil {
				body, _ = io.ReadAll(req.Body)
			}

			api.mu.Lock()
			api.trace = append(api.trace, key+" | "+req.Header.Get("Authorization"))

			if api.bodies == nil {
				api.bodies = map[string]string{}
			}

			api.bodies[key] = string(body)
			api.mu.Unlock()

			reply, ok := api.replies[key]
			if key == api.failOn {
				reply = wireReply{status: api.failStatus, body: `{"message":"refused"}`}
			} else if !ok {
				api.t.Errorf("unexpected request %s", key)

				reply = wireReply{status: http.StatusNotFound, body: `{"message":"unexpected"}`}
			}

			header := http.Header{"Content-Type": {"application/json"}}
			if reply.next != "" {
				header.Set("Link", `<https://forgejo.invalid/api/v1/next?page=`+reply.next+`>; rel="next"`)
			}

			return &http.Response{StatusCode: reply.status, Header: header, Body: io.NopCloser(strings.NewReader(reply.body)), Request: req}, nil
		})},
	}
}

// TestListContainerPackageVersions_RequestsEachPageOnceWithItsFilters: a
// second page is requested exactly once, with the same page size and filters,
// and its matching version is part of the result. The single-page test cannot
// tell a loop that stops after page one from one that finishes.
func TestListContainerPackageVersions_RequestsEachPageOnceWithItsFilters(t *testing.T) {
	t.Parallel()

	api := &wireAPI{t: t, replies: map[string]wireReply{
		"GET /api/v1/packages/owner?limit=50&page=1&q=img&type=container": {
			status: http.StatusOK, next: "2",
			body: `[{"type":"container","name":"img","version":"a"},{"type":"container","name":"img-extra","version":"x"}]`,
		},
		"GET /api/v1/packages/owner?limit=50&page=2&q=img&type=container": {
			status: http.StatusOK,
			body:   `[{"type":"generic","name":"img","version":"y"},{"type":"container","name":"img","version":"b"}]`,
		},
	}}

	versions, err := api.provider().ListContainerPackageVersions(t.Context(), "owner", "img")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, versions)
	require.Equal(t, []string{
		"GET /api/v1/packages/owner?limit=50&page=1&q=img&type=container | token tok",
		"GET /api/v1/packages/owner?limit=50&page=2&q=img&type=container | token tok",
	}, api.trace)
}

// TestUploadReleaseAsset_ReplacesAnAttachmentListedOnTheSecondPage: the
// same-name attachment is only on the second page of the release's assets, so
// replacing it proves the listing was complete, and each page is requested
// once before the upload and the delete. The attachment name travels in the
// multipart body, which TestUploadReleaseAsset_UploadsUnderTheBasenameOnly pins.
func TestUploadReleaseAsset_ReplacesAnAttachmentListedOnTheSecondPage(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "app.tgz")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))

	api := &wireAPI{t: t, replies: map[string]wireReply{
		"GET /api/v1/repos/owner/repo/releases/tags/v1":                    {status: http.StatusOK, body: `{"id":42}`},
		"GET /api/v1/repos/owner/repo/releases/42/assets?limit=100&page=1": {status: http.StatusOK, body: `[{"id":1,"name":"other.txt"}]`, next: "2"},
		"GET /api/v1/repos/owner/repo/releases/42/assets?limit=100&page=2": {status: http.StatusOK, body: `[{"id":2,"name":"app.tgz"}]`},
		"POST /api/v1/repos/owner/repo/releases/42/assets":                 {status: http.StatusCreated, body: `{"id":3,"name":"app.tgz"}`},
		"DELETE /api/v1/repos/owner/repo/releases/42/assets/2":             {status: http.StatusNoContent},
	}}

	require.NoError(t, api.provider().UploadReleaseAsset(t.Context(), "v1", file))
	require.Equal(t, []string{
		"GET /api/v1/repos/owner/repo/releases/tags/v1 | token tok",
		"GET /api/v1/repos/owner/repo/releases/42/assets?limit=100&page=1 | token tok",
		"GET /api/v1/repos/owner/repo/releases/42/assets?limit=100&page=2 | token tok",
		"POST /api/v1/repos/owner/repo/releases/42/assets | token tok",
		"DELETE /api/v1/repos/owner/repo/releases/42/assets/2 | token tok",
	}, api.trace)
}

// TestUploadReleaseAsset_RefusesAnAttachmentListThatDoesNotAdvance: a server
// that keeps pointing at the page it just served is refused after that one
// request instead of being followed until the job times out, and nothing is
// uploaded.
func TestUploadReleaseAsset_RefusesAnAttachmentListThatDoesNotAdvance(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "app.tgz")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))

	api := &wireAPI{t: t, replies: map[string]wireReply{
		"GET /api/v1/repos/owner/repo/releases/tags/v1":                    {status: http.StatusOK, body: `{"id":42}`},
		"GET /api/v1/repos/owner/repo/releases/42/assets?limit=100&page=1": {status: http.StatusOK, body: `[]`, next: "1"},
	}}

	err := api.provider().UploadReleaseAsset(t.Context(), "v1", file)
	require.ErrorIs(t, err, errs.ErrMalformedInput)
	require.Len(t, api.trace, 2)
}

// TestForgejoRequests_RefusalsSendNothingAndTheControlSendsExactlyOne runs
// each input refusal against a transport that accepts anything and counts
// requests: none may be sent. The control is a valid tag deletion, whose one
// request is compared whole, so a transport that never records cannot pass.
func TestForgejoRequests_RefusalsSendNothingAndTheControlSendsExactlyOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, second := filepath.Join(dir, "a", "app.tgz"), filepath.Join(dir, "b", "app.tgz")

	for _, path := range []string{first, second} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("asset"), 0o600))
	}

	refusals := map[string]struct {
		run  func(context.Context, *forgejo.Provider) error
		want error
	}{
		"delete a digest-pinned ref": {want: errs.ErrUsage, run: func(ctx context.Context, p *forgejo.Provider) error {
			return p.DeleteTag(ctx, "forgejo.invalid/owner/img@sha256:"+strings.Repeat("a", 64))
		}},
		"bot permissions for a repo without owner": {want: errs.ErrUsage, run: func(ctx context.Context, p *forgejo.Provider) error {
			_, err := p.ValidateBotPermissions(ctx, "repo")

			return err
		}},
		"validate an empty token": {want: errs.ErrPermissionDenied, run: func(ctx context.Context, p *forgejo.Provider) error {
			return p.ValidateToken(ctx, "", "owner/repo")
		}},
		"upload with an empty tag": {want: errs.ErrUsage, run: func(ctx context.Context, p *forgejo.Provider) error {
			return p.UploadReleaseAsset(ctx, "", first)
		}},
		"publish two assets with one basename": {want: errs.ErrValidation, run: func(ctx context.Context, p *forgejo.Provider) error {
			return p.PublishRelease(ctx, "owner/repo", provider.ReleaseSpec{Tag: "v1", Assets: []string{first, second}})
		}},
		"create without a repository": {want: errs.ErrUsage, run: func(ctx context.Context, p *forgejo.Provider) error {
			return p.CreateRelease(ctx, "", provider.ReleaseSpec{Tag: "v1"})
		}},
	}

	for name, refusal := range refusals {
		api := &wireAPI{t: t}
		require.ErrorIs(t, refusal.run(t.Context(), api.provider()), refusal.want, name)
		require.Empty(t, api.trace, name)
	}

	api := &wireAPI{t: t, replies: map[string]wireReply{
		"DELETE /api/v1/packages/owner/container/img/old": {status: http.StatusNoContent},
	}}

	require.NoError(t, api.provider().DeleteTag(t.Context(), "forgejo.invalid/owner/img:old"))
	require.Equal(t, []string{"DELETE /api/v1/packages/owner/container/img/old | token tok"}, api.trace)
}

// TestForgejoRelease_EachFailureStopsAtItsRequest runs both release
// strategies and fails every request in turn. Publish over an existing
// release: lookup, attachment listing, upload, collision delete, stale delete,
// metadata update. Recreate: lookup, delete of the existing release, create,
// upload. Each failure ends the trace at the failed request with that
// response's class; in publish the metadata update stays last, so any earlier
// failure leaves the old notes. The success runs compare both whole traces and
// the create and update payloads, including the draft and prerelease policy.
func TestForgejoRelease_EachFailureStopsAtItsRequest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file, notes := filepath.Join(dir, "app.tgz"), filepath.Join(dir, "notes.md")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))
	require.NoError(t, os.WriteFile(notes, []byte("new notes"), 0o600))

	const release = "/api/v1/repos/owner/repo/releases"

	publish := []string{
		"GET " + release + "/tags/v1",
		"GET " + release + "/42/assets?limit=100&page=1",
		"POST " + release + "/42/assets",
		"DELETE " + release + "/42/assets/1",
		"DELETE " + release + "/42/assets/2",
		"PATCH " + release + "/42",
	}
	recreate := []string{
		"GET " + release + "/tags/v1",
		"DELETE " + release + "/42",
		"POST " + release,
		"POST " + release + "/43/assets",
	}
	replies := map[string]wireReply{
		publish[0]:  {status: http.StatusOK, body: `{"id":42,"tag_name":"v1"}`},
		publish[1]:  {status: http.StatusOK, body: `[{"id":1,"name":"app.tgz"},{"id":2,"name":"stale.txt"}]`},
		publish[2]:  {status: http.StatusCreated, body: `{"id":3,"name":"app.tgz"}`},
		publish[3]:  {status: http.StatusNoContent},
		publish[4]:  {status: http.StatusNoContent},
		publish[5]:  {status: http.StatusOK, body: `{"id":42,"tag_name":"v1"}`},
		recreate[1]: {status: http.StatusNoContent},
		recreate[2]: {status: http.StatusCreated, body: `{"id":43,"tag_name":"v1"}`},
		recreate[3]: {status: http.StatusCreated, body: `{"id":4,"name":"app.tgz"}`},
	}

	spec := provider.ReleaseSpec{Tag: "v1", Name: "One", NotesFile: notes, Prerelease: true, Assets: []string{file}}
	strategies := map[string]struct {
		steps []string
		run   func(context.Context, *forgejo.Provider) error
		body  string
	}{
		"publish": {steps: publish, body: `{"tag_name":"v1","target_commitish":"","name":"One","body":"new notes","draft":false,"prerelease":true}`,
			run: func(ctx context.Context, p *forgejo.Provider) error { return p.PublishRelease(ctx, "owner/repo", spec) }},
		"recreate": {steps: recreate, body: `{"tag_name":"v1","target_commitish":"","name":"One","body":"new notes","draft":false,"prerelease":true}`,
			run: func(ctx context.Context, p *forgejo.Provider) error { return p.CreateRelease(ctx, "owner/repo", spec) }},
	}

	statuses := []struct {
		status int
		want   error
	}{
		{http.StatusServiceUnavailable, errs.ErrDependencyUnavailable},
		{http.StatusForbidden, errs.ErrPermissionDenied},
		{http.StatusConflict, errs.ErrValidation},
	}

	for name, strategy := range strategies {
		api := &wireAPI{t: t, replies: replies}
		require.NoError(t, strategy.run(t.Context(), api.provider()), name)

		want := make([]string, 0, len(strategy.steps))
		for _, step := range strategy.steps {
			want = append(want, step+" | token tok")
		}

		require.Equal(t, want, api.trace, name)

		written := strategy.steps[len(strategy.steps)-1]
		if name == "recreate" {
			written = recreate[2]
		}

		require.JSONEq(t, strategy.body, api.bodies[written], name)

		for index, step := range strategy.steps {
			failure := statuses[index%len(statuses)]
			failing := &wireAPI{t: t, replies: replies, failOn: step, failStatus: failure.status}

			err := strategy.run(t.Context(), failing.provider())
			require.ErrorIs(t, err, failure.want, "%s failing %s", name, step)
			require.Equal(t, want[:index+1], failing.trace, "%s failing %s", name, step)
		}
	}
}

// TestListContainerPackageVersions_RefusesANextPageThatDoesNotAdvance covers
// the paging loop. A server that ignores the page parameter and keeps
// advertising rel="next" was followed until the job timed out, collecting
// the same page each time; the audit probe hit go test's ten-minute bound
// inside it. The second response is the first that can show the page did not
// advance, so the listing ends there as malformed, and a server that pages
// properly still yields every version once.
func TestListContainerPackageVersions_RefusesANextPageThatDoesNotAdvance(t *testing.T) {
	t.Parallel()

	serve := func(ignorePage bool) (*forgejo.Provider, *int) {
		requests := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++

			page := r.URL.Query().Get("page")
			if ignorePage {
				page = "1"
			}

			w.Header().Set("Content-Type", "application/json")

			switch page {
			case "2":
				_, _ = w.Write([]byte(`[{"id":2,"type":"container","name":"img","version":"v2"}]`))
			default:
				w.Header().Set("Link", `<https://forgejo.invalid/api/v1/packages/owner?page=2&limit=50>; rel="next"`)
				_, _ = w.Write([]byte(`[{"id":1,"type":"container","name":"img","version":"v1"}]`))
			}
		})

		return &forgejo.Provider{Env: envMap(map[string]string{"FORGEJO_TOKEN": "tok"}), HTTPClient: inMemoryClient(handler), APIBaseOverride: "https://forgejo.invalid"}, &requests
	}

	p, requests := serve(false)

	versions, err := p.ListContainerPackageVersions(t.Context(), "owner", "img")
	if err != nil || !slices.Equal(versions, []string{"v1", "v2"}) || *requests != 2 {
		t.Fatalf("paging server: versions=%v requests=%d err=%v", versions, *requests, err)
	}

	p, requests = serve(true)

	versions, err = p.ListContainerPackageVersions(t.Context(), "owner", "img")
	if !errors.Is(err, errs.ErrMalformedInput) || versions != nil || *requests != 2 {
		t.Fatalf("page-ignoring server: versions=%v requests=%d err=%v, want a malformed refusal after the second page", versions, *requests, err)
	}
}
