// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"crypto/md5" //nolint:gosec // asserting the protocol's transport checksum, not security.
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

const (
	runtimeToken = "rtok"
	artifactName = "dist"
)

type jsonObj = map[string]any

// runtimeServer fakes the Forgejo Actions runtime artifact service: the
// artifacts-list, file-container, and per-file endpoints. entries maps a
// container item path → file body. matchCount controls how many artifacts
// answer to the name (0 / 1 / many), exercising the exactly-one guard.
func runtimeServer(t *testing.T, entries map[string]string, matchCount int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		base := "http://" + r.Host

		switch r.URL.Path {
		case "/_apis/pipelines/workflows/7/artifacts":
			arts := make([]jsonObj, 0, matchCount)
			for range matchCount {
				arts = append(arts, jsonObj{"name": artifactName, "fileContainerResourceUrl": base + "/container"})
			}

			writeJSON(w, jsonObj{"value": arts})

		case "/container":
			vals := make([]jsonObj, 0, len(entries))
			for path, body := range entries {
				vals = append(vals, jsonObj{
					"path": path, "itemType": "file",
					"contentLocation": base + "/file?p=" + path, "fileLength": len(body),
				})
			}

			writeJSON(w, jsonObj{"value": vals})

		case "/file":
			body, ok := entries[r.URL.Query().Get("p")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			_, _ = w.Write([]byte(body))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // test fixture; the map values are always encodable.
}

func runtimeProvider(srv *httptest.Server) *forgejo.Provider {
	return &forgejo.Provider{
		Env: envMap(map[string]string{
			"ACTIONS_RUNTIME_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": runtimeToken,
			"FORGEJO_RUN_ID":        "7",
		}),
		HTTPClient: srv.Client(),
	}
}

func TestDownloadRunArtifact_HappyPath(t *testing.T) {
	srv := runtimeServer(t, map[string]string{
		"dist/a.txt":     "alpha",
		"dist/sub/b.txt": "bravo",
	}, 1)
	dir := t.TempDir()

	info, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	if info.FileCount != 2 || info.Bytes != int64(len("alpha")+len("bravo")) {
		t.Errorf("info = %+v, want 2 files / 10 bytes", info)
	}

	got, err := os.ReadFile(filepath.Join(dir, "sub", "b.txt"))
	if err != nil || string(got) != "bravo" {
		t.Errorf("nested file = %q, err=%v", got, err)
	}
}

func TestDownloadRunArtifact_CrossRunUnsupported(t *testing.T) {
	srv := runtimeServer(t, map[string]string{"dist/a.txt": "x"}, 1)

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(),
		provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir(), RunID: "9"})
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported (runtime token is run-scoped)", err)
	}
}

func TestDownloadRunArtifact_MatchCount(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		srv := runtimeServer(t, map[string]string{"dist/a.txt": "x"}, 0)

		_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
		if !errors.Is(err, errs.ErrReleaseNotFound) {
			t.Fatalf("err = %v, want ErrReleaseNotFound", err)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		srv := runtimeServer(t, map[string]string{"dist/a.txt": "x"}, 2)

		_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

// TestDownloadRunArtifact_RejectsTraversal proves the shared SafeJoin gate
// stops a malicious container entry from escaping the destination.
func TestDownloadRunArtifact_RejectsTraversal(t *testing.T) {
	srv := runtimeServer(t, map[string]string{"dist/../escape.txt": "evil"}, 1)
	dir := t.TempDir()

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (traversal rejected)", err)
	}

	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); statErr == nil {
		t.Error("traversal wrote a file outside the destination")
	}
}

func TestDownloadRunArtifact_RuntimeCredsRequired(t *testing.T) {
	p := &forgejo.Provider{Env: envMap(map[string]string{"FORGEJO_RUN_ID": "7"})} // no ACTIONS_RUNTIME_*

	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Fatalf("err = %v, want ErrCIRuntimeRequired (missing runtime creds)", err)
	}
}

// uploadRecorder captures what the v3 upload protocol sent: each PUT's
// itemPath→body, and the finalized size.
type uploadRecorder struct {
	mu        sync.Mutex
	puts      map[string]string
	putWire   map[string]putWire
	finalSize int64
}

// putWire captures the transport shape of one PUT, so tests can pin what
// the real Forgejo backend sees (its chunk saver 500s on chunked encoding
// — invisible in body-level assertions because httptest decodes chunked
// transparently).
type putWire struct {
	contentLength    int64
	transferEncoding []string
	contentRange     string
}

// uploadServer fakes the v3 create → PUT → finalize endpoints.
func uploadServer(t *testing.T, rec *uploadRecorder) *httptest.Server {
	t.Helper()

	rec.puts = map[string]string{}
	rec.putWire = map[string]putWire{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/_apis/pipelines/workflows/7/artifacts":
			writeJSON(w, jsonObj{"fileContainerResourceUrl": "http://" + r.Host + "/upload"})

		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			body, _ := io.ReadAll(r.Body)

			rec.mu.Lock()
			rec.puts[r.URL.Query().Get("itemPath")] = string(body)
			rec.putWire[r.URL.Query().Get("itemPath")] = putWire{
				contentLength:    r.ContentLength,
				transferEncoding: r.TransferEncoding,
				contentRange:     r.Header.Get("Content-Range"),
			}
			rec.mu.Unlock()

			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodPatch && r.URL.Path == "/_apis/pipelines/workflows/7/artifacts":
			var final struct {
				Size int64 `json:"Size"`
			}

			_ = json.NewDecoder(r.Body).Decode(&final)

			rec.mu.Lock()
			rec.finalSize = final.Size
			rec.mu.Unlock()

			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func uploadProvider(srv *httptest.Server) *forgejo.Provider {
	return &forgejo.Provider{
		Env: envMap(map[string]string{
			"ACTIONS_RUNTIME_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": runtimeToken,
			"FORGEJO_RUN_ID":        "7",
		}),
		HTTPClient: srv.Client(),
	}
}

func TestUploadRunArtifact_DirPreservesTree(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha")
	mustWrite(t, filepath.Join(dir, "sub", "b.txt"), "bravo")

	var rec uploadRecorder

	srv := uploadServer(t, &rec)

	info, err := uploadProvider(srv).UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	if info.FileCount != 2 || info.Bytes != int64(len("alpha")+len("bravo")) {
		t.Errorf("info = %+v, want 2 files / 10 bytes", info)
	}

	if rec.puts["dist/a.txt"] != "alpha" || rec.puts["dist/sub/b.txt"] != "bravo" {
		t.Errorf("PUT bodies = %v; want tree-preserving item paths", rec.puts)
	}

	if rec.finalSize != info.Bytes {
		t.Errorf("finalized size = %d, want %d (total bytes)", rec.finalSize, info.Bytes)
	}
}

func TestUploadRunArtifact_FilesFlattenToBasename(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "nested", "deep", "c.txt")
	mustWrite(t, f, "charlie")

	var rec uploadRecorder

	srv := uploadServer(t, &rec)

	if _, err := uploadProvider(srv).UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Files: []string{f}}); err != nil {
		t.Fatal(err)
	}

	if rec.puts["dist/c.txt"] != "charlie" {
		t.Errorf("Files mode should flatten to basename; puts = %v", rec.puts)
	}
}

func TestUploadRunArtifact_IfNoFiles(t *testing.T) {
	var rec uploadRecorder

	srv := uploadServer(t, &rec)
	ctx := context.Background()

	// Default (error) on an empty dir.
	if _, err := uploadProvider(srv).UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: provider.IfNoFilesError}); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("empty dir / error policy: err = %v, want ErrValidation", err)
	}

	// Ignore on an empty dir succeeds with nothing uploaded.
	info, err := uploadProvider(srv).UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: provider.IfNoFilesIgnore})
	if err != nil || info.FileCount != 0 {
		t.Errorf("ignore policy: info=%+v err=%v", info, err)
	}

	if len(rec.puts) != 0 {
		t.Errorf("ignore policy uploaded files: %v", rec.puts)
	}
}

func TestUploadRunArtifact_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "real.txt"), "data")

	if err := os.Symlink(filepath.Join(dir, "real.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	var rec uploadRecorder

	srv := uploadServer(t, &rec)

	info, err := uploadProvider(srv).UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}

	if info.FileCount != 1 || rec.puts["dist/link.txt"] != "" {
		t.Errorf("symlink should be skipped; files=%d puts=%v", info.FileCount, rec.puts)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUploadRunArtifact_BackendProtocolContract pins the four upload details the
// artifact backend (Codeberg) rejects uploads without: CreateArtifact carries
// RetentionDays; the per-file PUT joins itemPath with '&' when the container URL
// already has a query (as the backend's fileContainerResourceUrl does, via
// ?retentionDays=N); each PUT sends x-actions-results-md5 = base64(md5(body));
// and a zero-byte file gets Content-Range "bytes 0--1/0".
func TestUploadRunArtifact_BackendProtocolContract(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "data.bin"), "payload")
	mustWrite(t, filepath.Join(dir, "empty"), "")

	type putRec struct{ rawQuery, md5, contentRange string }

	var (
		mu         sync.Mutex
		createDays int64
		puts       = map[string]putRec{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/artifacts"):
			var body struct {
				RetentionDays int64 `json:"RetentionDays"`
			}

			_ = json.NewDecoder(r.Body).Decode(&body)

			mu.Lock()
			createDays = body.RetentionDays
			mu.Unlock()

			// Mirror Codeberg: the container URL already carries a query.
			writeJSON(w, jsonObj{"fileContainerResourceUrl": "http://" + r.Host + "/upload?retentionDays=1"})

		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			_, _ = io.Copy(io.Discard, r.Body)

			mu.Lock()
			puts[r.URL.Query().Get("itemPath")] = putRec{
				rawQuery:     r.URL.RawQuery,
				md5:          r.Header.Get("x-actions-results-md5"),
				contentRange: r.Header.Get("Content-Range"),
			}
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	p := &forgejo.Provider{
		Env: envMap(map[string]string{
			"ACTIONS_RUNTIME_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": runtimeToken,
			"FORGEJO_RUN_ID":        "7",
		}),
		HTTPClient: srv.Client(),
	}

	if _, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir, RetentionDays: 5}); err != nil {
		t.Fatal(err)
	}

	if createDays != 5 {
		t.Errorf("CreateArtifact RetentionDays = %d, want 5", createDays)
	}

	md5b64 := func(content string) string {
		sum := md5.Sum([]byte(content)) //nolint:gosec // protocol checksum.

		return base64.StdEncoding.EncodeToString(sum[:])
	}

	data := puts["dist/data.bin"]
	if !strings.Contains(data.rawQuery, "retentionDays=1&itemPath=") {
		t.Errorf("PUT query = %q, want itemPath joined with '&' after the existing query", data.rawQuery)
	}

	if data.md5 != md5b64("payload") {
		t.Errorf("data.bin md5 = %q, want %q", data.md5, md5b64("payload"))
	}

	if data.contentRange != "bytes 0-6/7" {
		t.Errorf("data.bin Content-Range = %q, want bytes 0-6/7", data.contentRange)
	}

	empty := puts["dist/empty"]
	if empty.contentRange != "bytes 0--1/0" {
		t.Errorf("empty Content-Range = %q, want bytes 0--1/0", empty.contentRange)
	}

	if empty.md5 != md5b64("") {
		t.Errorf("empty md5 = %q, want %q (md5 of empty)", empty.md5, md5b64(""))
	}
}

// TestUploadRunArtifact_EmptyFileWireShape pins the regression found by the
// first live Codeberg round-trip: a zero-byte file must go out with an
// explicit Content-Length: 0 and the empty-form Content-Range — NEVER
// Transfer-Encoding: chunked, which net/http silently switches to when
// ContentLength is 0 on a non-nil body and which Forgejo's chunk saver
// rejects with HTTP 500.
func TestUploadRunArtifact_EmptyFileWireShape(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "empty-marker"), "")
	mustWrite(t, filepath.Join(dir, "sized.txt"), "payload")

	var rec uploadRecorder

	srv := uploadServer(t, &rec)

	if _, err := uploadProvider(srv).UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir}); err != nil {
		t.Fatal(err)
	}

	empty, ok := rec.putWire["dist/empty-marker"]
	if !ok {
		t.Fatalf("no PUT recorded for the empty file; puts = %v", rec.puts)
	}

	if len(empty.transferEncoding) != 0 {
		t.Errorf("empty file sent with Transfer-Encoding %v; Forgejo 500s on chunked uploads", empty.transferEncoding)
	}

	if empty.contentLength != 0 {
		t.Errorf("empty file ContentLength = %d, want explicit 0 (a server sees -1 for chunked)", empty.contentLength)
	}

	if empty.contentRange != "bytes 0--1/0" {
		t.Errorf("empty file Content-Range = %q, want the canonical empty form", empty.contentRange)
	}

	sized := rec.putWire["dist/sized.txt"]
	if sized.contentLength != int64(len("payload")) || len(sized.transferEncoding) != 0 {
		t.Errorf("sized file wire shape = %+v, want plain Content-Length upload", sized)
	}
}
