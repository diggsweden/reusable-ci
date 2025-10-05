// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakegitserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServer_InMemorySnapshotOwnership(t *testing.T) {
	t.Parallel()

	server := &Server{t: t, routes: map[string]Handler{}}
	require.Empty(t, server.Requests())

	want := []Request{
		{Method: http.MethodPost, Path: "/repos/owner%2Frepo/releases", Body: []byte("first\n\x00"),
			Header: http.Header{"X-Fixture": {"one", "two"}}, Query: map[string][]string{"tag": {"first", "second"}, "blank": {""}}},
		{Method: http.MethodPut, Path: "/repos/other/releases", Body: []byte("second\n"),
			Header: http.Header{"X-Fixture": {"three", "four"}}, Query: map[string][]string{"tag": {"first", "second"}, "blank": {""}}},
	}
	handled := 0

	for _, expected := range want {
		server.On(expected.Method, expected.Path, func(req Request) Response {
			handled++

			require.Equal(t, expected, req)
			req.Header["X-Fixture"][0] = "handler mutation"
			req.Header.Set("X-Added", "handler")
			req.Query["tag"][1] = "handler mutation"
			delete(req.Query, "blank")
			req.Body[0] = 'X'

			return Response{Status: http.StatusAccepted, Body: "handled"}
		})
		body := slices.Clone(expected.Body)
		req := httptest.NewRequestWithContext(t.Context(), expected.Method, expected.Path+"?tag=first&tag=second&blank=", bytes.NewReader(body))
		req.Header = expected.Header.Clone()
		response := httptest.NewRecorder()
		server.handle(response, req)
		require.Equal(t, http.StatusAccepted, response.Code)
		require.Equal(t, "handled", response.Body.String())

		req.Header["X-Fixture"][1] = "caller mutation"
		req.URL.RawQuery = "tag=caller"
		body[0] = 'Y'
	}

	require.Equal(t, 2, handled)

	got := server.Requests()
	require.Equal(t, want, got)

	for index := range got {
		got[index].Method = "DELETE"
		got[index].Path = "/changed"
		got[index].Header["X-Fixture"][1] = "snapshot mutation"
		delete(got[index].Header, "X-Fixture")
		got[index].Query["tag"][0] = "snapshot mutation"
		got[index].Query["added"] = []string{"snapshot"}
		got[index].Body[1] = 'Z'
	}

	require.Equal(t, want, server.Requests())
}

func TestServer_InMemorySnapshotNilAndEmpty(t *testing.T) {
	t.Parallel()

	want := []Request{
		{},
		{Header: http.Header{}, Body: []byte{}, Query: map[string][]string{}},
		{Header: http.Header{"X-Nil": nil, "X-Empty": {}}, Query: map[string][]string{"nil": nil, "empty": {}}},
	}
	server := &Server{requests: want}
	require.Equal(t, want, server.Requests())
}
