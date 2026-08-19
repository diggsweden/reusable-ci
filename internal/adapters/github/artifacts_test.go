// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakegitserver"
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

// serveArtifactZip wires the three REST endpoints (list → 302 → blob) that
// return the given zip for artifact id 42 named "build-artifacts".
func serveArtifactZip(t *testing.T, zipBody []byte) *fakegitserver.Server {
	t.Helper()

	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusOK, Body: `{"total_count":1,"artifacts":[{"id":42,"name":"build-artifacts"}]}`}
	})
	srv.OnGet("/repos/owner/repo/actions/artifacts/42/zip", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusFound, Header: http.Header{"Location": []string{srv.URL() + "/blob/42"}}}
	})
	srv.OnGet("/blob/42", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusOK, Body: string(zipBody)}
	})

	return srv
}

// TestProvider_DownloadRunArtifact exercises the RunArtifactDownloader role
// (the forge-neutral entry point) and asserts the byte/file totals.
func TestProvider_DownloadRunArtifact(t *testing.T) {
	t.Parallel()

	zipBody := buildZip(t, map[string]string{"hello.txt": "world", "nested/inside.json": `{"ok":true}`})
	srv := serveArtifactZip(t, zipBody)

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(),
	}

	dir := t.TempDir()

	info, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Name:  "build-artifacts",
		Dir:   dir,
		RunID: "9876",
	})
	if err != nil {
		t.Fatalf("DownloadRunArtifact: %v", err)
	}

	if info.FileCount != 2 || info.Bytes != int64(len("world")+len(`{"ok":true}`)) {
		t.Errorf("info = %+v, want 2 files / %d bytes", info, len("world")+len(`{"ok":true}`))
	}
}

// servePatternArtifacts wires a run listing two glob-matching artifacts
// (sbom-a id 42, sbom-b id 43) plus a non-matching one (other id 99), each
// with its own one-file zip.
func servePatternArtifacts(t *testing.T) *fakegitserver.Server {
	t.Helper()

	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusOK, Body: `{"total_count":3,"artifacts":[{"id":42,"name":"sbom-a"},{"id":43,"name":"sbom-b"},{"id":99,"name":"other"}]}`}
	})

	for id, payload := range map[int]string{42: "AAA", 43: "BBB"} {
		zipBody := buildZip(t, map[string]string{"file.txt": payload})
		blob := fmt.Sprintf("/blob/%d", id)
		srv.OnGet(fmt.Sprintf("/repos/owner/repo/actions/artifacts/%d/zip", id), func(_ fakegitserver.Request) fakegitserver.Response {
			return fakegitserver.Response{Status: http.StatusFound, Header: http.Header{"Location": []string{srv.URL() + blob}}}
		})
		srv.OnGet(blob, func(_ fakegitserver.Request) fakegitserver.Response {
			return fakegitserver.Response{Status: http.StatusOK, Body: string(zipBody)}
		})
	}

	return srv
}

// TestProvider_DownloadRunArtifact_RejectsHostileForgeName proves the
// pattern-download path fails closed on a forge-supplied artifact name that
// would traverse out of Dir or carry control characters — before any blob is
// fetched, and writing nothing outside the destination.
func TestProvider_DownloadRunArtifact_RejectsHostileForgeName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "traversal", body: `{"total_count":1,"artifacts":[{"id":7,"name":".."}]}`},
		{name: "control_char", body: `{"total_count":1,"artifacts":[{"id":7,"name":"evil\u001bX"}]}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			srv := fakegitserver.New(t)
			srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusOK, Body: testCase.body}
			})

			p := &github.Provider{
				Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
				APIBaseOverride: srv.URL(),
			}

			dir := t.TempDir()

			_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Pattern: "*", Dir: dir, RunID: "9876"})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation (hostile forge name rejected)", err)
			}

			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("a rejected download must write nothing into dir; got %v", entries)
			}
		})
	}
}

// TestProvider_DownloadRunArtifact_PatternMergeMultiple downloads every
// artifact matching the glob and flattens them into the destination.
func TestProvider_DownloadRunArtifact_PatternMergeMultiple(t *testing.T) {
	t.Parallel()

	srv := servePatternArtifacts(t)
	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(),
	}

	dir := t.TempDir()

	info, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Pattern:       "sbom-*",
		MergeMultiple: true,
		Dir:           dir,
		RunID:         "9876",
	})
	if err != nil {
		t.Fatalf("DownloadRunArtifact: %v", err)
	}

	// Both matches contributed one file each (the non-match "other" is skipped).
	if info.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2 (both sbom-* matches, merged)", info.FileCount)
	}

	// Merge flattens into dir — the single file.txt is the last writer's content.
	if _, statErr := os.Stat(filepath.Join(dir, "file.txt")); statErr != nil {
		t.Errorf("merged file.txt missing: %v", statErr)
	}
}

// TestProvider_DownloadRunArtifact_PatternSeparateDirs places each match
// under its own <name>/ subdirectory.
func TestProvider_DownloadRunArtifact_PatternSeparateDirs(t *testing.T) {
	t.Parallel()

	srv := servePatternArtifacts(t)
	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(),
	}

	dir := t.TempDir()

	if _, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Pattern: "sbom-*",
		Dir:     dir,
		RunID:   "9876",
	}); err != nil {
		t.Fatalf("DownloadRunArtifact: %v", err)
	}

	for name, want := range map[string]string{"sbom-a": "AAA", "sbom-b": "BBB"} {
		got, err := os.ReadFile(filepath.Join(dir, name, "file.txt")) //nolint:gosec // test fixture
		if err != nil {
			t.Fatalf("%s/file.txt: %v", name, err)
		}

		if string(got) != want {
			t.Errorf("%s/file.txt = %q, want %q", name, got, want)
		}
	}
}

// TestProvider_DownloadRunArtifact_RejectsZipTraversal proves the shared
// SafeJoin guard now protects the GitHub zip path too: a malicious entry
// escaping the destination is refused and writes nothing outside.
// buildZipWithSymlink returns a zip carrying one symlink entry pointing
// at target, plus one ordinary file. A symlink is stored as an entry
// whose mode has fs.ModeSymlink and whose body is the link target.
func buildZipWithSymlink(t *testing.T, linkName, target string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	hdr := &zip.FileHeader{Name: linkName, Method: zip.Deflate}
	hdr.SetMode(fs.ModeSymlink | 0o777)

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}

	if _, writeErr := w.Write([]byte(target)); writeErr != nil {
		t.Fatalf("write link target: %v", writeErr)
	}

	plain, err := zw.Create("ordinary.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, writeErr := plain.Write([]byte("data")); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return buf.Bytes()
}

// TestProvider_DownloadRunArtifact_RejectsZipSymlink covers the guard the
// traversal test cannot reach. A symlink entry carries no ".." in its own
// name, so SafeJoin passes it; the escape happens when the link is
// followed — either by a later entry in the same archive writing through
// it, or by a build step reading the extracted tree.
//
// extractZipInto rejects symlink entries outright, and nothing asserted
// that. The upload side has an equivalent test (forgejo
// TestUploadRunArtifact_SkipsSymlinks); this is the download side.
func TestProvider_DownloadRunArtifact_RejectsZipSymlink(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, link, target string }{
		{name: "absolute target", link: "innocent.txt", target: "/etc/passwd"},
		{name: "relative escape", link: "innocent.txt", target: "../../outside"},
		{name: "directory link", link: "subdir", target: ".."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := serveArtifactZip(t, buildZipWithSymlink(t, tc.link, tc.target))

			p := &github.Provider{
				Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
				APIBaseOverride: srv.URL(),
			}

			dir := t.TempDir()

			_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "build-artifacts", Dir: dir, RunID: "9876"})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation (symlink entry rejected)", err)
			}

			// Refused for the whole archive, not just the one entry: the
			// ordinary file alongside it must not be left behind either,
			// or a caller could act on a partial extraction.
			if _, statErr := os.Stat(filepath.Join(dir, "ordinary.txt")); statErr == nil {
				t.Error("extracted a sibling entry from an archive that was refused")
			}

			if _, statErr := os.Lstat(filepath.Join(dir, tc.link)); statErr == nil {
				t.Error("the symlink itself was created")
			}
		})
	}
}

func TestProvider_DownloadRunArtifact_RejectsZipTraversal(t *testing.T) {
	t.Parallel()

	zipBody := buildZip(t, map[string]string{"../escape.txt": "evil"})
	srv := serveArtifactZip(t, zipBody)

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(),
	}

	dir := t.TempDir()

	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "build-artifacts", Dir: dir, RunID: "9876"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (zip traversal rejected)", err)
	}

	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); statErr == nil {
		t.Error("zip traversal wrote a file outside the destination")
	}
}
