// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package fakegitserver_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/fakegitserver"
)

func TestServer_RoutesGET(t *testing.T) {
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{
			Status: 200,
			Body:   `{"description":"x","license":{"spdx_id":"Apache-2.0"}}`,
		}
	})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/repos/owner/repo", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Apache-2.0") {
		t.Errorf("body = %q, want substring %q", body, "Apache-2.0")
	}
}

func TestServer_RoutesPOST(t *testing.T) {
	srv := fakegitserver.New(t)

	var captured fakegitserver.Request

	srv.OnPost("/repos/owner/repo/releases", func(r fakegitserver.Request) fakegitserver.Response {
		captured = r

		return fakegitserver.Response{Status: 201, Body: `{"tag_name":"v1.0.0"}`}
	})

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
		srv.URL()+"/repos/owner/repo/releases", strings.NewReader(`{"tag_name":"v1.0.0"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}

	if !strings.Contains(string(captured.Body), "v1.0.0") {
		t.Errorf("body not captured: %q", captured.Body)
	}
}

func TestServer_404_OnUnregistered(t *testing.T) {
	srv := fakegitserver.New(t)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/random", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServer_RecordsRequests(t *testing.T) {
	srv := fakegitserver.New(t)
	srv.OnGet("/x", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: 200, Body: "ok"}
	})

	for range 3 {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/x", nil)
		resp, _ := http.DefaultClient.Do(req)
		_ = resp.Body.Close()
	}

	got := srv.Requests()
	if len(got) != 3 {
		t.Errorf("Requests() = %d, want 3", len(got))
	}
}
