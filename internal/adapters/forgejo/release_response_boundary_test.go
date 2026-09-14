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
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestReleaseResponseBoundary_CreatedIDBeforeUpload(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{}`, `{"id":-1}`, `null`, `{"id":42}`} {
		asset := filepath.Join(t.TempDir(), "asset.tgz")
		if err := os.WriteFile(asset, []byte("owned bytes"), 0o600); err != nil {
			t.Fatal(err)
		}

		var calls []string

		client := &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.Method+" "+req.URL.Path)
			rec := httptest.NewRecorder()

			switch req.Method + " " + req.URL.Path {
			case "GET /api/v1/repos/owner/repo/releases/tags/v1.2.3":
				rec.WriteHeader(http.StatusNotFound)
			case "POST /api/v1/repos/owner/repo/releases":
				rec.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(rec, body)
			case "POST /api/v1/repos/owner/repo/releases/42/assets":
				rec.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(rec, `{"id":7}`)
			default:
				t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
				rec.WriteHeader(http.StatusBadRequest)
			}

			response := rec.Result()
			response.Request = req

			return response, nil
		})}
		p := &forgejo.Provider{HTTPClient: client, APIBaseOverride: "https://forgejo.invalid", Env: envMap(nil)}
		err := p.CreateRelease(t.Context(), "owner/repo", provider.ReleaseSpec{Tag: "v1.2.3", Assets: []string{asset}})
		want := []string{"GET /api/v1/repos/owner/repo/releases/tags/v1.2.3", "POST /api/v1/repos/owner/repo/releases"}

		if body == `{"id":42}` {
			if err != nil {
				t.Fatal(err)
			}

			want = append(want, "POST /api/v1/repos/owner/repo/releases/42/assets")
		} else if !errors.Is(err, errs.ErrMalformedInput) {
			t.Fatalf("body=%s err=%v", body, err)
		}

		if !slices.Equal(calls, want) {
			t.Fatalf("body=%s calls=%v want=%v", body, calls, want)
		}
	}
}

func TestReleaseResponseBoundary_StaleListBeforeAnyDelete(t *testing.T) {
	t.Parallel()

	for _, item := range []string{`null`, `{"id":0,"name":"bad"}`, `{"id":-1,"name":"bad"}`, `{"id":8,"name":"second"}`} {
		var deletes []string

		client := &http.Client{Transport: releaseAssetTransport(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()

			switch req.Method {
			case http.MethodGet:
				if strings.HasSuffix(req.URL.Path, "/assets") {
					_, _ = io.WriteString(rec, `[{"id":7,"name":"first"},`+item+`]`)
				} else {
					_, _ = io.WriteString(rec, `{"id":42}`)
				}
			case http.MethodPatch:
				_, _ = io.WriteString(rec, `{"id":42}`)
			case http.MethodDelete:
				deletes = append(deletes, req.URL.Path)

				rec.WriteHeader(http.StatusNoContent)
			default:
				t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
				rec.WriteHeader(http.StatusBadRequest)
			}

			response := rec.Result()
			response.Request = req

			return response, nil
		})}
		p := &forgejo.Provider{HTTPClient: client, APIBaseOverride: "https://forgejo.invalid", Env: envMap(nil)}

		err := p.PublishRelease(t.Context(), "owner/repo", provider.ReleaseSpec{Tag: "v1.2.3"})
		if item == `{"id":8,"name":"second"}` {
			if err != nil || !slices.Equal(deletes, []string{"/api/v1/repos/owner/repo/releases/42/assets/7", "/api/v1/repos/owner/repo/releases/42/assets/8"}) {
				t.Fatalf("positive err=%v deletes=%v", err, deletes)
			}
		} else if !errors.Is(err, errs.ErrMalformedInput) || len(deletes) != 0 {
			t.Fatalf("item=%s err=%v deletes=%v", item, err, deletes)
		}
	}
}
