// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakegitlabserver_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitlabserver"
)

// do sends one request through the server's client and returns the status,
// headers and body, failing on any transport or read error before the response
// is used.
func do(t *testing.T, srv *fakegitlabserver.Server, method, path, body string) (int, http.Header, string) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL()+path, reader)
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, resp.Header, string(got)
}

// TestServer_RoutesAndRecordsDistinctRequests sends three distinguishable
// requests and compares each response and each recorded request whole: an
// explicit status with repeated headers, the default status for a zero Status,
// a lower-case method registration matching an upper-case request, an escaped
// path matched as written, and a query and body recorded per request. A route
// registered twice answers with its last handler, and an unregistered route is
// a 404 naming the method and path.
func TestServer_RoutesAndRecordsDistinctRequests(t *testing.T) {
	t.Parallel()

	srv := fakegitlabserver.New(t)
	srv.OnPost("/api/v4/projects/owner%2Frepo/releases", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: http.StatusTeapot, Body: "replaced"}
	})
	srv.OnPost("/api/v4/projects/owner%2Frepo/releases", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: http.StatusCreated, Header: http.Header{"X-Fixture": {"one", "two"}}, Body: `{"id":1}`}
	})
	srv.On("get", "/api/v4/projects/x", func(fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `{"name":"repo"}`}
	})

	status, header, body := do(t, srv, http.MethodPost, "/api/v4/projects/owner%2Frepo/releases?draft=true&draft=false", "tag=v1&notes=raw bytes")
	require.Equal(t, http.StatusCreated, status)
	require.Equal(t, []string{"one", "two"}, header.Values("X-Fixture"))
	require.JSONEq(t, `{"id":1}`, body)

	status, _, body = do(t, srv, http.MethodGet, "/api/v4/projects/x", "")
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"name":"repo"}`, body)

	status, _, body = do(t, srv, http.MethodDelete, "/api/v4/projects/x", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "no route registered for DELETE /api/v4/projects/x\n", body)

	requests := srv.Requests()
	require.Len(t, requests, 3)

	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/api/v4/projects/owner%2Frepo/releases", requests[0].Path)
	require.Equal(t, map[string][]string{"draft": {"true", "false"}}, requests[0].Query)
	require.Equal(t, []byte("tag=v1&notes=raw bytes"), requests[0].Body)

	require.Equal(t, http.MethodGet, requests[1].Method)
	require.Equal(t, "/api/v4/projects/x", requests[1].Path)
	require.Empty(t, requests[1].Query)
	require.Empty(t, requests[1].Body)

	require.Equal(t, http.MethodDelete, requests[2].Method)
}
