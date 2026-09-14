// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
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

type replacementState struct {
	old, replacement string
	calls            []string
}

func replacementClient(t *testing.T, mode string) (*http.Client, *replacementState) {
	t.Helper()

	state := &replacementState{old: "original"}
	client := &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
		state.calls = append(state.calls, req.Method)
		rec := httptest.NewRecorder()

		switch req.Method + " " + req.URL.Path {
		case "GET /api/v1/repos/fixture/repo/releases/tags/v1.2.3":
			_, _ = io.WriteString(rec, `{"id":42}`)
		case "GET /api/v1/repos/fixture/repo/releases/42/assets":
			_, _ = io.WriteString(rec, `[{"id":7,"name":"app.tgz"},{"id":8,"name":"keep.txt"}]`)
		case "POST /api/v1/repos/fixture/repo/releases/42/assets":
			if state.old != "original" {
				t.Error("old bytes removed before successful upload")
			}

			if mode == "upload failure" {
				rec.WriteHeader(http.StatusForbidden)

				break
			}

			part, header, err := req.FormFile("attachment")
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = part.Close() }()

			body, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}

			if header.Filename != "app.tgz" {
				t.Errorf("name=%q", header.Filename)
			}

			state.replacement = string(body)

			rec.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(rec, `{"id":9,"name":"app.tgz"}`)
		case "DELETE /api/v1/repos/fixture/repo/releases/42/assets/7":
			if state.replacement != "replacement" {
				t.Error("delete preceded upload")
			}

			if mode == "delete failure" {
				rec.WriteHeader(http.StatusForbidden)

				break
			}

			state.old = ""

			rec.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			rec.WriteHeader(http.StatusBadRequest)
		}

		response := rec.Result()
		response.Request = req

		return response, nil
	})}

	return client, state
}

func TestUploadReleaseAsset_ReplacementPreservesOldBytesUntilUpload(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode, old, replacement, calls string
		want                          error
	}{
		{"success", "", "replacement", "GET GET POST DELETE", nil},
		{"upload failure", "original", "", "GET GET POST", errs.ErrPermissionDenied},
		{"delete failure", "original", "replacement", "GET GET POST DELETE", errs.ErrPermissionDenied},
		{"missing file", "original", "", "", os.ErrNotExist},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "app.tgz")
			if tc.mode != "missing file" {
				if err := os.WriteFile(file, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			client, state := replacementClient(t, tc.mode)
			adapter := &forgejo.Provider{HTTPClient: client, APIBaseOverride: "https://forgejo.invalid", Env: envMap(map[string]string{"FORGEJO_REPOSITORY": "fixture/repo"})}

			err := adapter.UploadReleaseAsset(t.Context(), "v1.2.3", file)
			if !errors.Is(err, tc.want) {
				t.Errorf("err=%v, want=%v", err, tc.want)
			}

			if state.old != tc.old || state.replacement != tc.replacement {
				t.Errorf("old=%q new=%q, want old=%q new=%q", state.old, state.replacement, tc.old, tc.replacement)
			}

			if strings.Join(state.calls, " ") != tc.calls {
				t.Errorf("calls=%v, want=%s", state.calls, tc.calls)
			}
		})
	}
}
