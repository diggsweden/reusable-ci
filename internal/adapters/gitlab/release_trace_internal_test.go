// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// scriptedStep is one expected request, "METHOD request-uri", and its answer.
type scriptedStep struct {
	request string
	status  int
	body    string
}

// scriptedGitLab answers requests only in the scripted order; failAt replaces
// that step's status. A request out of order or past the script fails the test.
type scriptedGitLab struct {
	t      *testing.T
	steps  []scriptedStep
	failAt int
	fail   int

	mu    sync.Mutex
	trace []string
}

func (api *scriptedGitLab) provider() *Provider {
	return &Provider{
		APIBaseOverride: "https://gitlab.invalid",
		Env: func(key string) string {
			return map[string]string{"GITLAB_TOKEN": "synthetic", "CI_PROJECT_PATH": "owner/repo"}[key]
		},
		HTTPClient: &http.Client{Transport: guardTransport(func(req *http.Request) (*http.Response, error) {
			if req.Body != nil {
				_, _ = io.Copy(io.Discard, req.Body)
			}

			api.mu.Lock()
			defer api.mu.Unlock()

			key := req.Method + " " + req.URL.RequestURI()
			index := len(api.trace)
			api.trace = append(api.trace, key)

			if index >= len(api.steps) || api.steps[index].request != key {
				api.t.Errorf("request %d = %s, not the scripted one", index, key)

				return &http.Response{StatusCode: http.StatusTeapot, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
			}

			step := api.steps[index]
			if index == api.failAt {
				step.status, step.body = api.fail, `{"message":"refused"}`
			}

			return &http.Response{StatusCode: step.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(step.body)), Request: req}, nil
		})},
	}
}

func (api *scriptedGitLab) requests() []string {
	out := make([]string, 0, len(api.steps))
	for _, step := range api.steps {
		out = append(out, step.request)
	}

	return out
}

const gitlabRelease = "/api/v4/projects/owner%2Frepo/releases/v1"

// TestPublishRelease_EachFailureStopsAtItsRequest publishes one asset over an
// existing release that has a same-name link and a stale one, and fails each
// request of the flow in turn: the lookup, the update, the upload, the
// collision listing, the in-place link update, the stale listing and the stale
// delete. Each failure ends the trace at the request that failed, with that
// response's error class; nothing later is sent, and no link is deleted before
// its replacement exists. The full script is the success control.
func TestPublishRelease_EachFailureStopsAtItsRequest(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "app.tgz")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))

	links := gitlabRelease + "/assets/links"
	steps := []scriptedStep{
		{request: "GET " + gitlabRelease, status: http.StatusOK, body: `{"tag_name":"v1"}`},
		{request: "PUT " + gitlabRelease, status: http.StatusOK, body: `{}`},
		{request: "POST /api/v4/projects/owner%2Frepo/uploads", status: http.StatusCreated, body: `{"full_path":"/-/project/1/uploads/abc/app.tgz"}`},
		{request: "GET " + links + "?per_page=100&page=1", status: http.StatusOK, body: `[{"id":7,"name":"app.tgz"},{"id":8,"name":"stale.txt"}]`},
		{request: "PUT " + links + "/7", status: http.StatusOK, body: `{"id":7,"name":"app.tgz"}`},
		{request: "GET " + links + "?per_page=100&page=1", status: http.StatusOK, body: `[{"id":7,"name":"app.tgz"},{"id":8,"name":"stale.txt"}]`},
		{request: "DELETE " + links + "/8", status: http.StatusNoContent},
	}

	spec := provider.ReleaseSpec{Tag: "v1", Name: "One", Assets: []string{file}}

	success := &scriptedGitLab{t: t, steps: steps, failAt: -1}
	require.NoError(t, success.provider().PublishRelease(t.Context(), "owner/repo", spec))
	require.Equal(t, success.requests(), success.trace)

	failures := []struct {
		status int
		want   error
	}{
		{http.StatusServiceUnavailable, errs.ErrDependencyUnavailable},
		{http.StatusForbidden, errs.ErrPermissionDenied},
		{http.StatusInternalServerError, errs.ErrDependencyUnavailable},
		{http.StatusServiceUnavailable, errs.ErrDependencyUnavailable},
		{http.StatusConflict, errs.ErrValidation},
		{http.StatusServiceUnavailable, errs.ErrDependencyUnavailable},
		{http.StatusForbidden, errs.ErrPermissionDenied},
	}

	for index, failure := range failures {
		t.Run(strconv.Itoa(index)+" "+steps[index].request, func(t *testing.T) {
			t.Parallel()

			api := &scriptedGitLab{t: t, steps: steps, failAt: index, fail: failure.status}
			err := api.provider().PublishRelease(t.Context(), "owner/repo", spec)
			require.ErrorIs(t, err, failure.want)
			require.Equal(t, api.requests()[:index+1], api.trace)
		})
	}
}

// TestReleaseLinks_ALaterPageIsReadForReplacementAndStaleRemoval: GitLab
// paginates asset links. A full first page must lead to the second, where the
// same-name link to repoint and the last stale link are, and both scans ask
// for the largest page GitLab allows.
func TestReleaseLinks_ALaterPageIsReadForReplacementAndStaleRemoval(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "app.tgz")
	require.NoError(t, os.WriteFile(file, []byte("asset"), 0o600))

	full := make([]string, 0, gitlabPageSize)
	for id := 1; id <= gitlabPageSize; id++ {
		full = append(full, fmt.Sprintf(`{"id":%d,"name":"old-%d"}`, id, id))
	}

	firstPage := "[" + strings.Join(full, ",") + "]"
	links := gitlabRelease + "/assets/links"

	steps := []scriptedStep{
		{request: "POST /api/v4/projects/owner%2Frepo/uploads", status: http.StatusCreated, body: `{"full_path":"/-/project/1/uploads/abc/app.tgz"}`},
		{request: "GET " + links + "?per_page=100&page=1", status: http.StatusOK, body: firstPage},
		{request: "GET " + links + "?per_page=100&page=2", status: http.StatusOK, body: `[{"id":500,"name":"app.tgz"}]`},
		{request: "PUT " + links + "/500", status: http.StatusOK, body: `{"id":500,"name":"app.tgz"}`},
	}

	api := &scriptedGitLab{t: t, steps: steps, failAt: -1}
	require.NoError(t, api.provider().UploadReleaseAsset(t.Context(), "v1", file))
	require.Equal(t, api.requests(), api.trace)

	stale := []scriptedStep{
		{request: "GET " + links + "?per_page=100&page=1", status: http.StatusOK, body: firstPage},
		{request: "GET " + links + "?per_page=100&page=2", status: http.StatusOK, body: `[{"id":500,"name":"app.tgz"},{"id":502,"name":"stale-last"}]`},
	}
	for id := 1; id <= gitlabPageSize; id++ {
		stale = append(stale, scriptedStep{request: "DELETE " + links + "/" + strconv.Itoa(id), status: http.StatusNoContent})
	}

	stale = append(stale, scriptedStep{request: "DELETE " + links + "/502", status: http.StatusNoContent})

	reconcile := &scriptedGitLab{t: t, steps: stale, failAt: -1}
	p := reconcile.provider()
	apiBase, headers := p.apiContext()
	require.NoError(t, p.deleteStaleReleaseLinks(t.Context(), apiBase, "owner%2Frepo", "v1", []string{file}, headers))
	require.Equal(t, reconcile.requests(), reconcile.trace)
}
