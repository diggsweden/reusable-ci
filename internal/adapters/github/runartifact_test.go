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
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
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
	t.Parallel()

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

// TestUploadRunArtifact_RetentionBecomesTheCreateExpiry: the requested
// retention used to be dropped, so every GitHub artifact got the repository
// default. Zero still sends no expiry; a positive retention sends expiresAt
// that many days out; a request beyond $GITHUB_RETENTION_DAYS is capped to it,
// as actions/upload-artifact does.
func TestUploadRunArtifact_RetentionBecomesTheCreateExpiry(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		days, maxDays, wantDays int
	}{
		{days: 0},
		{days: 7, wantDays: 7},
		{days: 90, maxDays: 30, wantDays: 30},
		{days: 10, maxDays: 30, wantDays: 10},
	} {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "a.txt"), "alpha")

		token := fakeRuntimeToken("run-1", "job-1")

		var rec uploadV4Recorder

		srv := uploadV4Server(t, &rec, token)
		env := map[string]string{"ACTIONS_RESULTS_URL": srv.URL, "ACTIONS_RUNTIME_TOKEN": token}

		if tc.maxDays > 0 {
			env["GITHUB_RETENTION_DAYS"] = strconv.Itoa(tc.maxDays)
		}

		before := time.Now().UTC().Truncate(time.Second)

		p := &github.Provider{Env: envFunc(env), HTTPClient: srv.Client()}
		if _, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir, RetentionDays: tc.days}); err != nil {
			t.Fatalf("retention %d: %v", tc.days, err)
		}

		after := time.Now().UTC()

		raw, present := rec.createReq["expiresAt"]
		if tc.wantDays == 0 {
			if present {
				t.Errorf("retention 0 sent expiresAt %v", raw)
			}

			continue
		}

		text, _ := raw.(string)

		expiry, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatalf("retention %d: expiresAt %v is not RFC 3339: %v", tc.days, raw, err)
		}

		if expiry.Before(before.AddDate(0, 0, tc.wantDays)) || expiry.After(after.AddDate(0, 0, tc.wantDays)) {
			t.Errorf("retention %d (max %d): expiresAt %s, want %d days after the upload", tc.days, tc.maxDays, expiry, tc.wantDays)
		}
	}
}

// The v4 protocol reads the workflow/job backend ids out of the runtime
// token's Actions.Results scope. A token without that scope cannot address
// the artifact service at all, so the upload must refuse locally — the
// unroutable ACTIONS_RESULTS_URL below would surface as a transport error,
// not ErrValidation, if any request went out.
func TestUploadRunArtifact_RejectsTokenWithoutActionsResultsScope(t *testing.T) {
	t.Parallel()

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

// TestUploadRunArtifact_IfNoFiles covers all three policies, and what each
// non-failing one returns in full.
//
// "warn" was the arm nobody executed, though it shares its branch with
// "ignore" and differs from "error" by the whole outcome of the step. The
// returned value matters too: a caller reports these totals as the upload
// result, so an ID or a byte count invented for an upload that never happened
// is a green step claiming an artifact a later job cannot download.
//
// The env accessor records every variable read. Both non-failing policies must
// return before the runtime context is resolved -- ACTIONS_RUNTIME_TOKEN is a
// credential, and reading it to decide something already decided widens where
// it travels. An empty env would make the credential lookup fail rather than
// prove it never happened.
func TestUploadRunArtifact_IfNoFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("error refuses", func(t *testing.T) {
		t.Parallel()

		env, read := recordingEnv(map[string]string{})

		p := &github.Provider{Env: env}
		if _, err := p.UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: provider.IfNoFilesError}); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("err = %v, want ErrValidation", err)
		}

		if got := read(); len(got) != 0 {
			t.Errorf("a refused upload read %v", got)
		}
	})

	for _, policy := range []provider.IfNoFilesPolicy{provider.IfNoFilesWarn, provider.IfNoFilesIgnore} {
		t.Run(string(policy)+" is a no-op", func(t *testing.T) {
			t.Parallel()

			env, read := recordingEnv(map[string]string{})

			p := &github.Provider{Env: env}

			info, err := p.UploadRunArtifact(ctx, provider.RunArtifactUpload{Name: "dist", Dir: t.TempDir(), IfNoFiles: policy})
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}

			if want := (provider.RunArtifactInfo{Name: "dist"}); info != want {
				t.Errorf("info = %+v, want %+v (name only, no id, no totals)", info, want)
			}

			for _, key := range read() {
				if strings.Contains(key, "TOKEN") || strings.Contains(key, "RESULTS") || strings.Contains(key, "RUNTIME") {
					t.Errorf("a no-op upload read the runtime credential variable %q", key)
				}
			}
		})
	}
}

// recordingEnv returns an env accessor over m plus a reader for the keys it
// was asked about, in order.
func recordingEnv(m map[string]string) (func(string) string, func() []string) {
	var (
		mu   sync.Mutex
		keys []string
	)

	return func(k string) string {
			mu.Lock()
			defer mu.Unlock()

			keys = append(keys, k)

			return m[k]
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()

			return append([]string(nil), keys...)
		}
}

func TestUploadRunArtifact_RejectsAggregateBeforeZipOrRuntimeResolution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, name)
		mustWriteFile(t, path, "")

		if err := os.Truncate(path, 4<<30); err != nil {
			t.Fatal(err)
		}
	}

	p := &github.Provider{Env: envFunc(map[string]string{})}

	_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "extracted bytes") {
		t.Fatalf("oversized upload error = %v, want aggregate validation before ZIP/runtime resolution", err)
	}
}

func TestUploadRunArtifact_RequiresResultsCreds(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: envFunc(map[string]string{})} // no ACTIONS_RESULTS_URL / token
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "f.txt"), "x")

	_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
	if !errors.Is(err, errs.ErrCIRuntimeRequired) {
		t.Fatalf("err = %v, want ErrCIRuntimeRequired (missing results creds)", err)
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

// v4Transport answers the results service and the blob store in memory and
// records, for every request, the host, method, path and Authorization
// header -- which is what the credential-audience assertions read.
type v4Transport struct {
	mu        sync.Mutex
	signedURL string
	requests  []string
}

func (v *v4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
	}

	v.mu.Lock()
	v.requests = append(v.requests, req.Method+" "+req.URL.Host+req.URL.Path+" auth="+req.Header.Get("Authorization"))
	v.mu.Unlock()

	body, status := `{}`, http.StatusOK

	switch {
	case strings.HasSuffix(req.URL.Path, "/CreateArtifact"):
		body = fmt.Sprintf(`{"ok":true,"signedUploadUrl":%q}`, v.signedURL)
	case strings.HasSuffix(req.URL.Path, "/FinalizeArtifact"):
		body = `{"ok":true,"artifactId":"99"}`
	case req.Method == http.MethodPut:
		status = http.StatusCreated
	}

	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TestUploadRunArtifact_KeepsTheTokenAndTheArtifactOnTheirOwnChannels checks
// who receives what. The runtime token goes only to the results service; the
// blob store gets the archive and no Authorization header. The upload URL the
// service hands back is refused when it would downgrade HTTPS to HTTP, carries
// credentials, has no host or uses another scheme -- it was accepted on its
// http:// or https:// prefix alone -- and then nothing is uploaded or
// finalized.
func TestUploadRunArtifact_KeepsTheTokenAndTheArtifactOnTheirOwnChannels(t *testing.T) {
	t.Parallel()

	const results = "https://results.example.invalid"

	token := fakeRuntimeToken("run-1", "job-2")
	bearer := "Bearer " + token
	twirp := "/twirp/github.actions.results.api.v1.ArtifactService/"

	for name, tc := range map[string]struct {
		resultsURL, signedURL string
		want                  []string
		wantErr               error
	}{
		"https blob store": {
			resultsURL: results, signedURL: "https://blob.example.invalid/c/dist.zip?sig=x",
			want: []string{
				"POST results.example.invalid" + twirp + "CreateArtifact auth=" + bearer,
				"PUT blob.example.invalid/c/dist.zip auth=",
				"POST results.example.invalid" + twirp + "FinalizeArtifact auth=" + bearer,
			},
		},
		"http blob store behind an http results service": {
			resultsURL: "http://forge.internal:3000", signedURL: "http://forge.internal:3000/blob?sig=x",
			want: []string{
				"POST forge.internal:3000" + twirp + "CreateArtifact auth=" + bearer,
				"PUT forge.internal:3000/blob auth=",
				"POST forge.internal:3000" + twirp + "FinalizeArtifact auth=" + bearer,
			},
		},
		"https downgraded to http": {resultsURL: results, signedURL: "http://blob.example.invalid/c/dist.zip?sig=x", wantErr: errs.ErrValidation},
		"credentials in the url":   {resultsURL: results, signedURL: "https://user:secret@blob.example.invalid/c?sig=x", wantErr: errs.ErrValidation}, //nolint:gosec // synthetic userinfo that must be refused.
		"no host":                  {resultsURL: results, signedURL: "https:///c/dist.zip", wantErr: errs.ErrValidation},
		"another scheme":           {resultsURL: results, signedURL: "ftp://blob.example.invalid/c", wantErr: errs.ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, "a.txt"), "alpha")

			transport := &v4Transport{signedURL: tc.signedURL}
			p := &github.Provider{
				Env:        envFunc(map[string]string{"ACTIONS_RESULTS_URL": tc.resultsURL, "ACTIONS_RUNTIME_TOKEN": token}),
				HTTPClient: &http.Client{Transport: transport},
			}

			_, err := p.UploadRunArtifact(context.Background(), provider.RunArtifactUpload{Name: "dist", Dir: dir})
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			want := tc.want
			if tc.wantErr != nil {
				want = []string{"POST results.example.invalid" + twirp + "CreateArtifact auth=" + bearer}
			}

			if !slices.Equal(transport.requests, want) {
				t.Errorf("requests =\n%q\nwant\n%q", transport.requests, want)
			}
		})
	}
}
