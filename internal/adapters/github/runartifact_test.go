// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// fakeRuntimeToken builds a JWT whose payload carries the Actions.Results
// scope the v4 upload decodes the backend ids from. Header/signature are
// dummy — only the payload is read.
func fakeRuntimeToken(run, job string) string {
	payload := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf(`{"scp":"Actions.Results:%s:%s Other.Scope:x"}`, run, job)))

	return "eyJhbGciOiJub25lIn0." + payload + ".sig"
}

// uploadV4Recorder captures the three stages so a test can assert the
// protocol is internally consistent (the finalized size/hash describe the
// blob actually PUT).
type uploadV4Recorder struct {
	mu          sync.Mutex
	createReq   map[string]any
	blobBody    []byte
	finalizeReq map[string]any
}

func uploadV4Server(t *testing.T, rec *uploadV4Recorder, token string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/blob" && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		body, _ := io.ReadAll(r.Body)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/twirp/github.actions.results.api.v1.ArtifactService/CreateArtifact":
			rec.mu.Lock()
			_ = json.Unmarshal(body, &rec.createReq)
			rec.mu.Unlock()

			_, _ = fmt.Fprintf(w, `{"ok":true,"signedUploadUrl":%q}`, "http://"+r.Host+"/blob?sig=x") //nolint:gosec // test fixture: r.Host is the test server, response is JSON not HTML.

		case r.Method == http.MethodPut && r.URL.Path == "/blob":
			rec.mu.Lock()
			rec.blobBody = body
			rec.mu.Unlock()

			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodPost && r.URL.Path == "/twirp/github.actions.results.api.v1.ArtifactService/FinalizeArtifact":
			rec.mu.Lock()
			_ = json.Unmarshal(body, &rec.finalizeReq)
			rec.mu.Unlock()

			_, _ = w.Write([]byte(`{"ok":true,"artifactId":"99"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestUploadRunArtifact_V4RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "alpha")
	mustWriteFile(t, filepath.Join(dir, "sub", "b.txt"), "bravo")

	token := fakeRuntimeToken("run-123", "job-456")

	var rec uploadV4Recorder

	srv := uploadV4Server(t, &rec, token)

	p := &github.Provider{
		Env: envFunc(map[string]string{
			"ACTIONS_RESULTS_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": token,
		}),
		HTTPClient: srv.Client(),
	}

	info, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if err != nil {
		t.Fatalf("UploadRunArtifact: %v", err)
	}

	if info.ID != "99" || info.FileCount != 2 || info.Bytes != int64(len("alpha")+len("bravo")) {
		t.Errorf("info = %+v, want id=99 / 2 files / 10 bytes", info)
	}

	// Backend ids were taken from the token's scp claim.
	if rec.createReq["workflowRunBackendId"] != "run-123" || rec.createReq["workflowJobRunBackendId"] != "job-456" {
		t.Errorf("create backend ids = %v", rec.createReq)
	}

	// The uploaded blob is a real zip carrying both files.
	assertZipHas(t, rec.blobBody, map[string]string{"a.txt": "alpha", "sub/b.txt": "bravo"})

	// Finalize describes exactly the blob that was PUT.
	wantHash := "sha256:" + fmt.Sprintf("%x", sha256.Sum256(rec.blobBody))
	if rec.finalizeReq["hash"] != wantHash {
		t.Errorf("finalize hash = %v, want %v", rec.finalizeReq["hash"], wantHash)
	}

	if int64(rec.finalizeReq["size"].(float64)) != int64(len(rec.blobBody)) { //nolint:forcetypeassert // JSON number.
		t.Errorf("finalize size = %v, want %d", rec.finalizeReq["size"], len(rec.blobBody))
	}
}

func TestBackendIDsFromToken_ViaUpload(t *testing.T) {
	// A token with no Actions.Results scope must fail before any network.
	p := &github.Provider{
		Env: envFunc(map[string]string{
			"ACTIONS_RESULTS_URL":   "https://example.invalid",
			"ACTIONS_RUNTIME_TOKEN": "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"scp":"Other:x"}`)) + ".sig",
		}),
	}

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "f.txt"), "x")

	_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (no Actions.Results scope)", err)
	}
}

func TestUploadRunArtifact_IfNoFiles(t *testing.T) {
	p := &github.Provider{Env: envFunc(map[string]string{})}
	ctx := context.Background()

	if _, err := p.UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: provider.IfNoFilesError}); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("error policy: err = %v, want ErrValidation", err)
	}

	info, err := p.UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: provider.IfNoFilesIgnore})
	if err != nil || info.FileCount != 0 {
		t.Errorf("ignore policy: info=%+v err=%v", info, err)
	}
}

func TestUploadRunArtifact_RequiresResultsCreds(t *testing.T) {
	p := &github.Provider{Env: envFunc(map[string]string{})} // no ACTIONS_RESULTS_URL / token
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "f.txt"), "x")

	_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage (missing results creds)", err)
	}
}

func assertZipHas(t *testing.T, body []byte, want map[string]string) {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("uploaded blob is not a zip: %v", err)
	}

	got := map[string]string{}

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}

		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[f.Name] = string(data)
	}

	for name, content := range want {
		if got[name] != content {
			t.Errorf("zip entry %q = %q, want %q (all: %v)", name, got[name], content, got)
		}
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
