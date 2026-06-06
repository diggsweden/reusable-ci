// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakegitlabserver_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/fakegitlabserver"
)

func TestServer_RoutesProjectGET(t *testing.T) {
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/owner%2Frepo", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{
			Status: 200,
			Body:   `{"description":"x","license":{"key":"apache-2.0"}}`,
		}
	})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/api/v4/projects/owner%2Frepo", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "apache-2.0") {
		t.Errorf("body = %q, want apache-2.0", body)
	}
}

func TestServer_RoutesPOSTAndPUT(t *testing.T) {
	srv := fakegitlabserver.New(t)
	srv.OnPost("/api/v4/projects/x/releases", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 201, Body: `{"tag_name":"v1.0.0"}`}
	})
	srv.OnPut("/api/v4/projects/x/releases/v1.0.0", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Status: 200, Body: `{}`}
	})

	reqPost, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
		srv.URL()+"/api/v4/projects/x/releases", strings.NewReader(`{"tag_name":"v1.0.0"}`))
	reqPost.Header.Set("Content-Type", "application/json")
	resp1, _ := http.DefaultClient.Do(reqPost)

	defer func() { _ = resp1.Body.Close() }()

	if resp1.StatusCode != http.StatusCreated {
		t.Errorf("POST status = %d, want 201", resp1.StatusCode)
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL()+"/api/v4/projects/x/releases/v1.0.0", nil)
	resp2, _ := http.DefaultClient.Do(req)

	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("PUT status = %d, want 200", resp2.StatusCode)
	}
}

func TestServer_404OnUnregistered(t *testing.T) {
	srv := fakegitlabserver.New(t)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/random", nil)
	resp, _ := http.DefaultClient.Do(req)

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServer_RequestsReturnsSnapshot(t *testing.T) {
	srv := fakegitlabserver.New(t)
	srv.OnGet("/api/v4/projects/x", func(_ fakegitlabserver.Request) fakegitlabserver.Response {
		return fakegitlabserver.Response{Body: `{}`}
	})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/api/v4/projects/x?with_license=true", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	requests := srv.Requests()
	if len(requests) != 1 {
		t.Fatalf("Requests len = %d, want 1", len(requests))
	}

	if got := requests[0].Query["with_license"]; len(got) != 1 || got[0] != "true" {
		t.Errorf("query = %q, want true", got)
	}

	requests[0].Path = "tampered"
	if got := srv.Requests()[0].Path; got != "/api/v4/projects/x" {
		t.Errorf("stored request path = %q, want original", got)
	}
}
