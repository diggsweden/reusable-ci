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
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
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
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
	}

	err := github.NewArtifactDownloader(p).DownloadArtifact(context.Background(), release.ArtifactDownloadInput{
		RunID: "9876",
		Name:  "missing",
		Dir:   t.TempDir(),
	})
	if !errors.Is(err, errs.ErrReleaseNotFound) {
		t.Fatalf("err = %v, want ErrReleaseNotFound", err)
	}

	if !strings.Contains(err.Error(), `"missing" not found`) {
		t.Errorf("err = %v, want the missing artifact named in the message", err)
	}
}

// A zero ArtifactDownloader has no Provider and therefore no client; the
// documented contract is that every call returns ErrUsage rather than
// panicking on the nil dereference.
func TestArtifactDownloader_NilProviderIsAUsageError(t *testing.T) {
	t.Parallel()

	err := github.ArtifactDownloader{}.DownloadArtifact(context.Background(), release.ArtifactDownloadInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
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

func TestProvider_DownloadRunArtifact_RejectsOversizedCompressedResponse(t *testing.T) {
	t.Parallel()

	srv := fakegitserver.New(t)
	srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusOK, Body: `{"total_count":1,"artifacts":[{"id":42,"name":"build-artifacts"}]}`}
	})
	srv.OnGet("/repos/owner/repo/actions/artifacts/42/zip", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{Status: http.StatusFound, Header: http.Header{"Location": []string{srv.URL() + "/blob/42"}}}
	})
	srv.OnGet("/blob/42", func(_ fakegitserver.Request) fakegitserver.Response {
		return fakegitserver.Response{
			Status: http.StatusOK,
			Header: http.Header{"Content-Length": []string{strconv.FormatInt(domainartifact.MaxArchiveBytes+1, 10)}},
		}
	})

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
	}

	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Name: "build-artifacts", Dir: t.TempDir(), RunID: "9876",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("oversized response error = %v, want ErrValidation", err)
	}
}

// TestProvider_DownloadRunArtifact exercises the RunArtifactDownloader role
// (the forge-neutral entry point) and asserts the byte/file totals.
func TestProvider_DownloadRunArtifact(t *testing.T) {
	t.Parallel()

	zipBody := buildZip(t, map[string]string{"hello.txt": "world", "nested/inside.json": `{"ok":true}`})
	srv := serveArtifactZip(t, zipBody)

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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

func TestProvider_DownloadRunArtifact_RejectsExistingDestinationSymlink(t *testing.T) {
	t.Parallel()

	srv := serveArtifactZip(t, buildZip(t, map[string]string{"hello.txt": "new"}))
	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
	}
	dir := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "hello.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Name: "build-artifacts", Dir: dir, RunID: "9876",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("existing destination symlink error = %v, want ErrValidation", err)
	}

	if got, readErr := os.ReadFile(outside); readErr != nil || string(got) != "outside" {
		t.Fatalf("outside target changed: body=%q err=%v", got, readErr)
	}

	info, err := os.Lstat(filepath.Join(dir, "hello.txt"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("rejected destination symlink changed: info=%v err=%v", info, err)
	}
}

func TestProvider_DownloadRunArtifact_RejectsSymlinkedDestinationRootOrAncestor(t *testing.T) {
	t.Parallel()

	srv := serveArtifactZip(t, buildZip(t, map[string]string{"hello.txt": "new"}))
	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
	}
	base := t.TempDir()

	realDest := filepath.Join(base, "real")
	if err := os.MkdirAll(realDest, 0o755); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(base, "linked")
	if err := os.Symlink(realDest, linked); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	for _, dir := range []string{linked, filepath.Join(linked, "child")} {
		_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
			Name: "build-artifacts", Dir: dir, RunID: "9876",
		})
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("destination %q error = %v, want ErrValidation", dir, err)
		}
	}

	if _, err := os.Stat(filepath.Join(realDest, "hello.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink destination was written through: %v", err)
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
				APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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

func TestProvider_DownloadRunArtifact_PatternZeroMatchesIsNoOp(t *testing.T) {
	t.Parallel()

	srv := servePatternArtifacts(t)
	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
	}
	dir := filepath.Join(t.TempDir(), "downloads")

	info, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
		Pattern: "missing-*",
		Dir:     dir,
		RunID:   "9876",
	})
	if err != nil {
		t.Fatalf("DownloadRunArtifact: %v", err)
	}

	if info.Name != "missing-*" || info.FileCount != 0 || info.Bytes != 0 {
		t.Errorf("zero-match info = %+v, want pattern name and zero totals", info)
	}

	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("zero-match download created destination or returned unexpected stat error: %v", statErr)
	}
}

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
				APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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

// TestProvider_DownloadRunArtifact_RejectsZipTraversal proves the shared
// SafeJoin guard protects the GitHub zip path too: a malicious entry
// escaping the destination is refused and writes nothing outside.
func TestProvider_DownloadRunArtifact_RejectsZipTraversal(t *testing.T) {
	t.Parallel()

	zipBody := buildZip(t, map[string]string{"../escape.txt": "evil"})
	srv := serveArtifactZip(t, zipBody)

	p := &github.Provider{
		Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
		APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
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

// spoolFileCount counts the artifact spool files currently in the temp
// directory the downloader uses.
func spoolFileCount(t *testing.T) int {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "reusable-ci-artifact-*.zip"))
	if err != nil {
		t.Fatal(err)
	}

	return len(matches)
}

// TestProvider_DownloadRunArtifact_RefusesWholeOperationAndCleansTheSpool
// covers each way the download can fail after it has started spooling.
//
// The downloader writes the archive to a temporary file before it will open it,
// because a zip cannot be read from a stream. That file is the operation's own
// state, and every failure path has to remove it — a runner that accumulates
// one spooled archive per failed download fills its disk with copies of release
// artifacts, readable by anything else on the box.
//
// The other half is that a refusal publishes nothing. These failures happen at
// different points — before the body is read, while reading it, and after it is
// fully written — and the destination has to be empty in all three.
//
//nolint:paralleltest // shares process-wide state (the spool directory / the colour policy).
func TestProvider_DownloadRunArtifact_RefusesWholeOperationAndCleansTheSpool(t *testing.T) {
	for _, tc := range []struct {
		name string
		blob func(srv *fakegitserver.Server) fakegitserver.Response
		want error
	}{
		{
			name: "the blob host refuses",
			blob: func(*fakegitserver.Server) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusForbidden}
			},
			want: errs.ErrPermissionDenied,
		},
		{
			name: "the blob host is unavailable",
			blob: func(*fakegitserver.Server) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusBadGateway}
			},
			want: errs.ErrDependencyUnavailable,
		},
		{
			name: "the body is not a zip at all",
			blob: func(*fakegitserver.Server) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusOK, Body: "this is not a zip archive"}
			},
			want: errs.ErrValidation,
		},
		{
			name: "the body is a truncated zip",
			blob: func(*fakegitserver.Server) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusOK, Body: "PK\x03\x04truncated"}
			},
			want: errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: the spool count is taken from the shared temp
			// directory, so a concurrent download would be counted too.
			before := spoolFileCount(t)

			srv := fakegitserver.New(t)
			srv.OnGet("/repos/owner/repo/actions/runs/9876/artifacts", func(_ fakegitserver.Request) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusOK, Body: `{"total_count":1,"artifacts":[{"id":42,"name":"build-artifacts"}]}`}
			})
			srv.OnGet("/repos/owner/repo/actions/artifacts/42/zip", func(_ fakegitserver.Request) fakegitserver.Response {
				return fakegitserver.Response{Status: http.StatusFound, Header: http.Header{"Location": []string{srv.URL() + "/blob/42"}}}
			})
			srv.OnGet("/blob/42", func(_ fakegitserver.Request) fakegitserver.Response { return tc.blob(srv) })

			dir := t.TempDir()

			p := &github.Provider{
				Env:             envFunc(map[string]string{"GITHUB_REPOSITORY": "owner/repo"}),
				APIBaseOverride: srv.URL(), HTTPClient: srv.Client(),
			}

			_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
				Name: "build-artifacts", Dir: dir, RunID: "9876",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if got := spoolFileCount(t); got != before {
				t.Errorf("spool files went from %d to %d; a failed download left its temporary archive behind", before, got)
			}

			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}

			if len(entries) != 0 {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}

				t.Errorf("a refused download published %v", names)
			}
		})
	}
}
