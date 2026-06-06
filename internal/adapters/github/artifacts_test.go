// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakegitserver"
)

// buildZip returns an in-memory ZIP containing the given files.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%q): %v", name, err)
		}

		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	return buf.Bytes()
}

func TestArtifactDownloader_FetchesAndExtractsZip(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)

	// 1. List run artifacts — return one named "build-artifacts" with ID 42.
	srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{
			Status: http.StatusOK,
			Body:   `{"total_count":1,"artifacts":[{"id":42,"name":"build-artifacts"}]}`,
		}
	})

	// 2. Download endpoint — 302 to the blob URL on the same fake server.
	srv.OnGet("/repos/owner/repo/actions/artifacts/42/zip", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{
			Status: http.StatusFound,
			Header: http.Header{"Location": []string{srv.URL() + "/blob/42"}},
		}
	})

	// 3. Blob URL — return the ZIP bytes.
	zipBody := buildZip(t, map[string]string{
		"hello.txt":          "world",
		"nested/inside.json": `{"ok":true}`,
	})

	srv.OnGet("/blob/42", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{
			Status: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"application/zip"}},
			Body:   string(zipBody),
		}
	})

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		APIBaseOverride: srv.URL(),
	}
	dl := github.NewArtifactDownloader(p)

	dir := t.TempDir()
	if err := dl.DownloadArtifact(context.Background(), release.ArtifactDownloadInput{
		RunID: "9876",
		Name:  "build-artifacts",
		Dir:   dir,
	}); err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}

	hello, err := os.ReadFile(filepath.Join(dir, "hello.txt")) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("hello.txt: %v", err)
	}

	if string(hello) != "world" {
		t.Errorf("hello.txt = %q, want %q", hello, "world")
	}

	inside, err := os.ReadFile(filepath.Join(dir, "nested", "inside.json")) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("nested/inside.json: %v", err)
	}

	if string(inside) != `{"ok":true}` {
		t.Errorf("inside.json = %q", inside)
	}
}

func TestArtifactDownloader_ArtifactNotFoundIsTypedError(t *testing.T) {
	t.Parallel()
	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusOK, Body: `{"total_count":0,"artifacts":[]}`}
	})

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(),
	}

	err := github.NewArtifactDownloader(p).DownloadArtifact(context.Background(), release.ArtifactDownloadInput{
		RunID: "9876",
		Name:  "missing",
		Dir:   t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), `"missing" not found`) {
		t.Fatalf("err = %v, want not-found", err)
	}
}

func TestArtifactDownloader_RejectsBadInput(t *testing.T) {
	t.Parallel()

	err := github.ArtifactDownloader{}.DownloadArtifact(context.Background(), release.ArtifactDownloadInput{})
	if err == nil {
		t.Fatal("expected error with nil Provider")
	}
}
