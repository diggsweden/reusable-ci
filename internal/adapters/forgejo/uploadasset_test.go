// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// UploadReleaseAsset was uncovered. It is how a signature, a checksum
// file or an SBOM reaches a published release, and it resolves the
// repository from the runner environment rather than from an argument --
// so a mis-resolved repository publishes a release artifact to the wrong
// project.

// assetUploadServer answers the two calls the upload makes: resolve the
// release by tag, then post the attachment. It records the attachment
// name and the paths it was asked for.
type assetUpload struct {
	releasePath string
	uploadPath  string
	filename    string
	body        string
	// rawBody is the multipart payload exactly as it went over the wire.
	// Go's multipart reader applies filepath.Base to Part.FileName(), so
	// the parsed header cannot distinguish a full path sent by the client
	// from a basename -- only the raw Content-Disposition can.
	rawBody string
}

func newAssetUploadServer(t *testing.T, rec *assetUpload) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/releases/tags/"):
			rec.releasePath = r.URL.Path

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":42,"tag_name":"v1.0.0"}`)

		case strings.Contains(r.URL.Path, "/assets"):
			rec.uploadPath = r.URL.Path

			if raw, err := io.ReadAll(r.Body); err == nil {
				rec.rawBody = string(raw)
				r.Body = io.NopCloser(strings.NewReader(rec.rawBody))
			}

			if file, header, err := r.FormFile("attachment"); err == nil {
				rec.filename = header.Filename

				body, _ := io.ReadAll(file)
				rec.body = string(body)

				_ = file.Close()
			}

			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":1,"name":"asset"}`)

		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))

	t.Cleanup(srv.Close)

	return srv
}

// TestUploadReleaseAsset_UploadsUnderTheBasenameOnly is the claim worth
// holding. The asset name sent to the forge is filepath.Base of the local
// path: the local layout ("dist/linux-amd64/app.tar.gz") is an artefact
// of the build, and sending it as the attachment name would either be
// rejected or publish an asset whose name carries directories.
func TestUploadReleaseAsset_UploadsUnderTheBasenameOnly(t *testing.T) {
	t.Parallel()

	var rec assetUpload

	srv := newAssetUploadServer(t, &rec)

	dir := t.TempDir()
	nested := filepath.Join(dir, "dist", "linux-amd64")

	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	asset := filepath.Join(nested, "app.tar.gz")
	if err := os.WriteFile(asset, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := &forgejo.Provider{
		Env: envMap(map[string]string{
			"FORGEJO_TOKEN":      "tok",
			"FORGEJO_REPOSITORY": "itiquette/gommitlint",
		}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}

	if err := provider.UploadReleaseAsset(context.Background(), "v1.0.0", asset); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}

	if rec.filename != "app.tar.gz" {
		t.Errorf("attachment name = %q, want app.tar.gz", rec.filename)
	}

	// The wire bytes, not the parsed header: Go's multipart reader
	// applies filepath.Base itself, so a client that sent the full path
	// would still parse as "app.tar.gz" here. Only the raw
	// Content-Disposition shows what was actually transmitted, and a
	// forge that does not normalise would publish it as sent.
	if !strings.Contains(rec.rawBody, `filename="app.tar.gz"`) {
		t.Errorf("Content-Disposition does not carry the bare basename:\n%s", firstLines(rec.rawBody, 4))
	}

	if strings.Contains(rec.rawBody, "dist/") || strings.Contains(rec.rawBody, "linux-amd64") {
		t.Errorf("the local directory layout went over the wire:\n%s", firstLines(rec.rawBody, 4))
	}

	// The repository came from the runner environment, not from a
	// hardcoded default.
	if !strings.Contains(rec.releasePath, "/itiquette/gommitlint/") {
		t.Errorf("release lookup path = %q, want the repository from the environment", rec.releasePath)
	}

	if rec.body != "payload" {
		t.Errorf("uploaded body = %q, want the file's contents", rec.body)
	}

	if rec.uploadPath != "/api/v1/repos/itiquette/gommitlint/releases/42/assets" {
		t.Errorf("attachment posted to %q, want the release resolved from the tag", rec.uploadPath)
	}
}

// TestUploadReleaseAsset_Refusals covers the inputs that must not reach
// the forge. An empty tag would resolve "the release named empty"; an
// unset repository would otherwise be split into empty owner and name
// and post to a path made of slashes.
func TestUploadReleaseAsset_Refusals(t *testing.T) {
	t.Parallel()

	asset := filepath.Join(t.TempDir(), "app.tar.gz")
	if err := os.WriteFile(asset, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		env  map[string]string
		tag  string
		file string
	}{
		{
			name: "no tag",
			env:  map[string]string{"FORGEJO_TOKEN": "tok", "FORGEJO_REPOSITORY": "itiquette/gommitlint"},
			tag:  "",
			file: asset,
		},
		{
			name: "no repository in the environment",
			env:  map[string]string{"FORGEJO_TOKEN": "tok"},
			tag:  "v1.0.0",
			file: asset,
		},
		{
			name: "a repository with no owner",
			env:  map[string]string{"FORGEJO_TOKEN": "tok", "FORGEJO_REPOSITORY": "gommitlint"},
			tag:  "v1.0.0",
			file: asset,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rec assetUpload

			srv := newAssetUploadServer(t, &rec)

			provider := &forgejo.Provider{
				Env:             envMap(tc.env),
				HTTPClient:      srv.Client(),
				APIBaseOverride: srv.URL,
			}

			err := provider.UploadReleaseAsset(context.Background(), tc.tag, tc.file)
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			if rec.uploadPath != "" {
				t.Errorf("an upload was attempted anyway, to %q", rec.uploadPath)
			}
		})
	}
}

// TestUploadReleaseAsset_MissingFileIsNotAnUpload keeps a local mistake
// from being reported as a forge failure, and from creating an empty
// attachment on a real release.
func TestUploadReleaseAsset_MissingFileIsNotAnUpload(t *testing.T) {
	t.Parallel()

	var rec assetUpload

	srv := newAssetUploadServer(t, &rec)

	provider := &forgejo.Provider{
		Env: envMap(map[string]string{
			"FORGEJO_TOKEN":      "tok",
			"FORGEJO_REPOSITORY": "itiquette/gommitlint",
		}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}

	err := provider.UploadReleaseAsset(context.Background(), "v1.0.0",
		filepath.Join(t.TempDir(), "not-built.tar.gz"))
	if err == nil {
		t.Fatal("uploading a file that does not exist was accepted")
	}

	if rec.uploadPath != "" {
		t.Errorf("an attachment was posted for a file that does not exist: %q", rec.uploadPath)
	}
}

// firstLines trims a body down to something legible in a failure.
func firstLines(body string, n int) string {
	lines := strings.Split(body, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}

	return strings.Join(lines, "\n")
}
