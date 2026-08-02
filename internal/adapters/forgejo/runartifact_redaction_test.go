// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestDownloadRunArtifact_FailsLoudWithoutLeakingToken locks two disciplines on
// the token-bearing HTTP adapter at once: it must FAIL LOUD on a non-2xx (no
// swallowed error) and must never leak the runtime Bearer token into the error
// it surfaces — the token lives only in the request header. A future refactor
// that interpolates the response or request into the error would trip this.
func TestDownloadRunArtifact_FailsLoudWithoutLeakingToken(t *testing.T) {
	const secretToken = "super-secret-runtime-token-do-not-log"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth must still be carried (proves the token reaches the wire)…
		if r.Header.Get("Authorization") != "Bearer "+secretToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		// …then fail server-side so the adapter must surface an error.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	prov := &forgejo.Provider{
		Env: envMap(map[string]string{
			"ACTIONS_RUNTIME_URL":   srv.URL,
			"ACTIONS_RUNTIME_TOKEN": secretToken,
			"FORGEJO_RUN_ID":        "7",
		}),
		HTTPClient: srv.Client(),
	}

	_, err := prov.DownloadRunArtifact(context.Background(),
		provider.RunArtifactDownload{Name: "dist", Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected a fail-loud error on HTTP 500, got nil")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should name the HTTP status (fail loud); got: %v", err)
	}

	if strings.Contains(err.Error(), secretToken) {
		t.Errorf("error leaked the runtime Bearer token: %v", err)
	}
}
