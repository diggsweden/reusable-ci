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
	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
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
// runtimeServerNamed is runtimeServer with the artifact name under the
// test's control, so a forge reporting a hostile name can be simulated.
// runtimeService is where a download test's provider reaches the runtime API:
// an in-memory handler at runtimeFakeURL for API semantics, or a loopback
// listener for the redirect tests that need two real origins.
type runtimeService struct {
	URL    string
	client *http.Client
}

// runtimeFakeURL is the runtime origin the in-memory fakes answer for.
const runtimeFakeURL = "https://runtime.forgejo.invalid"

// serveRuntime answers every runtime request with handler in memory.
func serveRuntime(handler http.Handler) runtimeService {
	return runtimeService{URL: runtimeFakeURL, client: inMemoryClient(handler)}
}

// listenerRuntime adapts a loopback server for the two-origin tests.
func listenerRuntime(srv *httptest.Server) runtimeService {
	return runtimeService{URL: srv.URL, client: srv.Client()}
}

func runtimeServerNamed(t *testing.T, name string, entries map[string]string) runtimeService {
	t.Helper()

	return runtimeServerWith(t, name, entries, 1)
}

func runtimeServer(t *testing.T, entries map[string]string, matchCount int) runtimeService {
	t.Helper()

	return runtimeServerWith(t, artifactName, entries, matchCount)
}

func runtimeServerWith(t *testing.T, name string, entries map[string]string, matchCount int) runtimeService {
	t.Helper()

	return serveRuntime(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		base := runtimeFakeURL

		switch r.URL.Path {
		case "/_apis/pipelines/workflows/7/artifacts":
			arts := make([]jsonObj, 0, matchCount)
			for range matchCount {
				arts = append(arts, jsonObj{"name": name, "fileContainerResourceUrl": base + "/container"})
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
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // test fixture; the map values are always encodable.
}

func runtimeProvider(srv runtimeService) *forgejo.Provider {
	return &forgejo.Provider{
		Env: envMap(map[string]string{
			"ACTIONS_RUNTIME_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": runtimeToken,
			"FORGEJO_RUN_ID":        "7",
		}),
		HTTPClient: srv.client,
	}
}

func TestDownloadRunArtifact_HappyPath(t *testing.T) {
	t.Parallel()

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

func TestDownloadRunArtifact_RejectsOversizedRuntimeJSON(t *testing.T) {
	t.Parallel()

	const maxRuntimeJSONBytes = 16 << 20

	srv := serveRuntime(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat(" ", maxRuntimeJSONBytes+1))
	}))

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("oversized runtime JSON error = %v, want ErrValidation", err)
	}
}

func TestDownloadRunArtifact_RejectsOversizedDeclaredTotalBeforeContentRequest(t *testing.T) {
	t.Parallel()

	contentRequested := false

	srv := serveRuntime(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_apis/pipelines/workflows/7/artifacts":
			writeJSON(w, jsonObj{"value": []jsonObj{{"name": "dist", "fileContainerResourceUrl": runtimeFakeURL + "/container"}}})
		case "/container":
			writeJSON(w, jsonObj{"value": []jsonObj{{
				"path": "dist/huge", "itemType": "file",
				"contentLocation": runtimeFakeURL + "/file", "fileLength": domainartifact.MaxTotalBytes + 1,
			}}})
		case "/file":
			contentRequested = true
			_, _ = io.WriteString(w, "unexpected")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("oversized declared total error = %v, want ErrValidation", err)
	}

	if contentRequested {
		t.Fatal("oversized declared artifact requested file content")
	}
}

func TestDownloadRunArtifact_RejectsExistingDestinationSymlink(t *testing.T) {
	t.Parallel()

	srv := runtimeServer(t, map[string]string{"dist/a.txt": "new"}, 1)
	dir := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(dir, "a.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("existing destination symlink error = %v, want ErrValidation", err)
	}

	if got, readErr := os.ReadFile(outside); readErr != nil || string(got) != "outside" {
		t.Fatalf("outside target changed: body=%q err=%v", got, readErr)
	}

	info, err := os.Lstat(filepath.Join(dir, "a.txt"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("rejected destination symlink changed: info=%v err=%v", info, err)
	}
}

func TestDownloadRunArtifact_RejectsSymlinkedDestinationRootOrAncestor(t *testing.T) {
	t.Parallel()

	srv := runtimeServer(t, map[string]string{"dist/a.txt": "new"}, 1)
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
		_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: dir})
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("destination %q error = %v, want ErrValidation", dir, err)
		}
	}

	if _, err := os.Stat(filepath.Join(realDest, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink destination was written through: %v", err)
	}
}

func TestDownloadRunArtifact_StripsTokenFromCrossOriginContentURL(t *testing.T) {
	t.Parallel()

	var receivedAuthorization string

	blob := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(blob.Close)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		base := "http://" + r.Host
		switch r.URL.Path {
		case "/_apis/pipelines/workflows/7/artifacts":
			writeJSON(w, jsonObj{"value": []jsonObj{{"name": "dist", "fileContainerResourceUrl": base + "/container"}}})
		case "/container":
			writeJSON(w, jsonObj{"value": []jsonObj{{
				"path": "dist/value", "itemType": "file", "contentLocation": blob.URL + "/value", "fileLength": 7,
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(runtime.Close)

	p := runtimeProvider(listenerRuntime(runtime))

	p.HTTPClient = blob.Client()
	if _, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}

	if receivedAuthorization != "" {
		t.Fatalf("cross-origin content request leaked Authorization %q", receivedAuthorization)
	}
}

func TestDownloadRunArtifact_TokenlessRedirectsStayHTTPSAndCredentialFree(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		headers []string
		blob    *httptest.Server
	)

	blob = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()

		headers = append(headers, r.Header.Get("Authorization"))
		mu.Unlock()

		switch r.URL.Path {
		case "/first":
			http.Redirect(w, r, blob.URL+"/second", http.StatusFound)
		case "/second":
			http.Redirect(w, r, blob.URL+"/value", http.StatusTemporaryRedirect)
		case "/value":
			_, _ = w.Write([]byte("payload"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(blob.Close)

	if err := downloadRuntimeContentURL(t, blob.URL+"/first", blob.Client()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(headers) != 3 {
		t.Fatalf("tokenless redirect requests = %d, want 3", len(headers))
	}

	for index, header := range headers {
		if header != "" {
			t.Errorf("tokenless redirect request %d leaked Authorization %q", index, header)
		}
	}
}

func TestDownloadRunArtifact_RejectsTokenlessRedirectToHTTP(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		called bool
	)

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		called = true
		mu.Unlock()

		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(plain.Close)

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/value", http.StatusFound)
	}))
	t.Cleanup(secure.Close)

	err := downloadRuntimeContentURL(t, secure.URL+"/first", secure.Client())
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("tokenless HTTP redirect error = %v, want ErrValidation", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if called {
		t.Fatal("tokenless HTTP redirect destination was requested")
	}
}

func TestDownloadRunArtifact_BoundsTokenlessRedirectCount(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests int
		blob     *httptest.Server
	)

	blob = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		http.Redirect(w, r, blob.URL+"/loop", http.StatusFound)
	}))
	t.Cleanup(blob.Close)

	err := downloadRuntimeContentURL(t, blob.URL+"/loop", blob.Client())
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("redirect loop error = %v, want ErrDependencyUnavailable", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if requests > 10 {
		t.Fatalf("redirect loop made %d requests, want at most 10", requests)
	}
}

func downloadRuntimeContentURL(t *testing.T, contentURL string, client *http.Client) error {
	t.Helper()

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		base := "http://" + r.Host
		switch r.URL.Path {
		case "/_apis/pipelines/workflows/7/artifacts":
			writeJSON(w, jsonObj{"value": []jsonObj{{"name": "dist", "fileContainerResourceUrl": base + "/container"}}})
		case "/container":
			writeJSON(w, jsonObj{"value": []jsonObj{{
				"path": "dist/value", "itemType": "file", "contentLocation": contentURL, "fileLength": 7,
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(runtime.Close)

	p := runtimeProvider(listenerRuntime(runtime))
	p.HTTPClient = client
	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})

	return err
}

func TestDownloadRunArtifact_RejectsCrossOriginAuthenticatedContainerURL(t *testing.T) {
	t.Parallel()

	called := false
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(foreign.Close)

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonObj{"value": []jsonObj{{"name": "dist", "fileContainerResourceUrl": foreign.URL + "/container"}}})
	}))
	t.Cleanup(runtime.Close)

	_, err := runtimeProvider(listenerRuntime(runtime)).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("cross-origin container URL error = %v, want ErrValidation", err)
	}

	if called {
		t.Fatal("cross-origin authenticated container URL was requested")
	}
}

func TestDownloadRunArtifact_RejectsInsecureNonLoopbackRuntimeURL(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: envMap(map[string]string{
		"ACTIONS_RUNTIME_URL":   "http://example.com",
		"ACTIONS_RUNTIME_TOKEN": runtimeToken,
		"FORGEJO_RUN_ID":        "7",
	})}

	_, err := p.DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("insecure runtime URL error = %v, want ErrValidation", err)
	}
}

func TestDownloadRunArtifact_CrossRunUnsupported(t *testing.T) {
	t.Parallel()

	srv := runtimeServer(t, map[string]string{"dist/a.txt": "x"}, 1)

	_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(),
		provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir(), RunID: "9"})
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported (runtime token is run-scoped)", err)
	}
}

func TestDownloadRunArtifact_MatchCount(t *testing.T) {
	t.Parallel()

	t.Run("zero", func(t *testing.T) {
		t.Parallel()

		srv := runtimeServer(t, map[string]string{"dist/a.txt": "x"}, 0)

		_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
		if !errors.Is(err, errs.ErrReleaseNotFound) {
			t.Fatalf("err = %v, want ErrReleaseNotFound", err)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		t.Parallel()

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
	t.Parallel()

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

// TestDownloadRunArtifact_RejectsHostileArtifactName covers the second
// SafeJoin site. There are two: one guards the per-file entry path inside
// an artifact, which RejectsTraversal above exercises, and one guards the
// artifact *name* the forge reports, which becomes a directory under the
// destination when several artifacts are downloaded without
// --merge-multiple.
//
// A name is not caller-controlled -- it comes back from the forge API --
// which is exactly why it is worth guarding and worth testing: a
// compromised or buggy forge is the case the check exists for. The github
// provider has the equivalent test
// (TestProvider_DownloadRunArtifact_RejectsHostileForgeName).
func TestDownloadRunArtifact_RejectsHostileArtifactName(t *testing.T) {
	t.Parallel()

	// path.Match selects the artifacts, and "*" does not cross a slash --
	// so only slash-free names reach the join at all. A name containing a
	// slash is filtered out before the guard, which is a second layer
	// rather than a gap.
	for _, name := range []string{
		// Reach SafeJoin: a traversal that survives glob matching.
		"..", `..\escape`, `..\..\escape`,

		// Reach ValidateName instead: a control character is not
		// traversal, so SafeJoin would pass it. This is why both guards
		// are there.
		//
		// ValidateName also rejects invalid UTF-8, which cannot be
		// exercised through this path: the name arrives as JSON, and
		// encoding/json substitutes U+FFFD for invalid bytes, so it is
		// already valid by the time it is checked. That guard defends a
		// non-JSON caller and is covered in domain/artifact.
		//
		// An ANSI escape is the input that separates them: it is not
		// traversal, so SafeJoin passes it, and the name is echoed into
		// the CI log and becomes a directory — a terminal-spoof vector.
		"dist\x00evil", "evil\x1b[31mRED",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := runtimeServerNamed(t, name, map[string]string{"file.txt": "data"})
			dir := t.TempDir()

			// Pattern, not Name: the by-name path writes straight into
			// Dir and never uses the artifact name as a directory, so the
			// guard only applies when several artifacts are matched and
			// each needs its own subdirectory.
			_, err := runtimeProvider(srv).DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
				Pattern: "*", Dir: dir,
			})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation (hostile artifact name rejected)", err)
			}

			if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); statErr == nil {
				t.Error("hostile artifact name created a directory outside the destination")
			}
		})
	}
}

func TestDownloadRunArtifact_RuntimeCredsRequired(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestUploadRunArtifact_FilesPreserveCommonRootRelativePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, "nested", "deep", "c.txt")
	second := filepath.Join(dir, "nested", "other", "d.txt")

	mustWrite(t, first, "charlie")
	mustWrite(t, second, "delta")

	var rec uploadRecorder

	srv := uploadServer(t, &rec)

	if _, err := uploadProvider(srv).UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Files: []string{second, first}}); err != nil {
		t.Fatal(err)
	}

	if rec.puts["dist/deep/c.txt"] != "charlie" || rec.puts["dist/other/d.txt"] != "delta" {
		t.Errorf("Files mode should preserve common-root paths; puts = %v", rec.puts)
	}
}

func TestUploadRunArtifact_IfNoFiles(t *testing.T) {
	t.Parallel()

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

func TestUploadRunArtifact_RejectsAggregateBeforeRuntimeAPI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, name)
		mustWrite(t, path, "")

		if err := os.Truncate(path, 4<<30); err != nil {
			t.Fatal(err)
		}
	}

	p := &forgejo.Provider{Env: envMap(map[string]string{})}

	_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "extracted bytes") {
		t.Fatalf("oversized upload error = %v, want aggregate validation before runtime credentials/API", err)
	}
}

func TestUploadRunArtifact_SkipsSymlinks(t *testing.T) {
	t.Parallel()

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

	if _, uploaded := rec.puts["dist/link.txt"]; info.FileCount != 1 || uploaded {
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
	t.Parallel()

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

	// Zero asks for the forge default, which Forgejo cannot take as absent.
	if _, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir}); err != nil {
		t.Fatal(err)
	}

	if createDays != 1 {
		t.Errorf("CreateArtifact RetentionDays for zero = %d, want the backend minimum 1", createDays)
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
	t.Parallel()

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

// multiArtifactServer fakes the runtime service with SEVERAL distinctly-named
// artifacts, each with its own file container. runtimeServer above serves one
// name repeated, which cannot exercise pattern matching across names.
//
// artifacts maps artifact name → (item path → body).
func multiArtifactServer(t *testing.T, artifacts map[string]map[string]string) runtimeService {
	t.Helper()

	return serveRuntime(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+runtimeToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		base := runtimeFakeURL

		switch {
		case r.URL.Path == "/_apis/pipelines/workflows/7/artifacts":
			arts := make([]jsonObj, 0, len(artifacts))
			for name := range artifacts {
				arts = append(arts, jsonObj{"name": name, "fileContainerResourceUrl": base + "/container/" + name})
			}

			writeJSON(w, jsonObj{"value": arts})

		case strings.HasPrefix(r.URL.Path, "/container/"):
			name := strings.TrimPrefix(r.URL.Path, "/container/")

			vals := make([]jsonObj, 0)
			for itemPath, body := range artifacts[name] {
				vals = append(vals, jsonObj{
					"path": itemPath, "itemType": "file",
					"contentLocation": base + "/file?a=" + name + "&p=" + itemPath,
					"fileLength":      len(body),
				})
			}

			writeJSON(w, jsonObj{"value": vals})

		case r.URL.Path == "/file":
			body, ok := artifacts[r.URL.Query().Get("a")][r.URL.Query().Get("p")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			_, _ = w.Write([]byte(body))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// TestDownloadRunArtifact_Pattern covers the pattern / merge-multiple path,
// which had no test at all: downloadMatchingContainers and
// resolveMatchingContainers were both at 0% coverage, so the glob, the
// per-artifact destination, and the flattening were unverified.
func TestDownloadRunArtifact_Pattern(t *testing.T) {
	t.Parallel()

	fixture := map[string]map[string]string{
		"dist-linux": {"bin/app": "linux-body"},
		"dist-mac":   {"bin/app": "mac-body"},
		"sboms":      {"sbom.json": "{}"}, // must NOT match dist-*
	}

	t.Run("each match lands under its own artifact dir", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		info, err := runtimeProvider(multiArtifactServer(t, fixture)).
			DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Pattern: "dist-*", Dir: dir})
		if err != nil {
			t.Fatalf("download: %v", err)
		}

		if info.FileCount != 2 {
			t.Errorf("FileCount = %d, want 2 (the non-matching artifact must be skipped)", info.FileCount)
		}

		for name, want := range map[string]string{"dist-linux": "linux-body", "dist-mac": "mac-body"} {
			got, readErr := os.ReadFile(filepath.Join(dir, name, "bin", "app"))
			if readErr != nil {
				t.Fatalf("%s: %v", name, readErr)
			}

			if string(got) != want {
				t.Errorf("%s = %q, want %q — the wrong container was downloaded for this name",
					name, got, want)
			}
		}

		if _, statErr := os.Stat(filepath.Join(dir, "sboms")); statErr == nil {
			t.Error("an artifact not matching the pattern was downloaded")
		}
	})

	t.Run("merge-multiple flattens into Dir", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		if _, err := runtimeProvider(multiArtifactServer(t, fixture)).
			DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{
				Pattern: "dist-*", Dir: dir, MergeMultiple: true,
			}); err != nil {
			t.Fatalf("download: %v", err)
		}

		// Both artifacts carry bin/app, so the flattened tree has one at the
		// top; the point is that no per-artifact directory was created.
		if _, err := os.Stat(filepath.Join(dir, "bin", "app")); err != nil {
			t.Errorf("merge-multiple did not flatten into Dir: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, "dist-linux")); err == nil {
			t.Error("merge-multiple still created a per-artifact directory")
		}
	})

	t.Run("zero matches are a no-op", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "downloads")

		info, err := runtimeProvider(multiArtifactServer(t, fixture)).
			DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Pattern: "missing-*", Dir: dir})
		if err != nil {
			t.Fatalf("download: %v", err)
		}

		if info.Name != "missing-*" || info.FileCount != 0 || info.Bytes != 0 {
			t.Errorf("zero-match info = %+v, want pattern name and zero totals", info)
		}

		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Errorf("zero-match download created destination or returned unexpected stat error: %v", statErr)
		}
	})

	t.Run("a bad glob is a usage error", func(t *testing.T) {
		t.Parallel()

		_, err := runtimeProvider(multiArtifactServer(t, fixture)).
			DownloadRunArtifact(context.Background(), provider.RunArtifactDownload{Pattern: "[", Dir: t.TempDir()})
		if !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})
}
