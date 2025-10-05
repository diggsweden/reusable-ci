// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package rawref_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest/rawref"
)

// roundTripFunc serves the oracle's requests from a handler in memory, so the
// wire shape it reads is exercised without a socket.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func inMemoryClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)

		return recorder.Result(), nil
	})}
}

// TestReleaseByTag_ReadsTheRecordedStateFromEachWire pins what the oracle
// reads off each forge: the identity fields, the policy flags Forgejo records
// (GitLab has none, so both stay false there), and the digest of the bytes
// each asset actually serves rather than the size the forge reports.
func TestReleaseByTag_ReadsTheRecordedStateFromEachWire(t *testing.T) {
	t.Parallel()

	const body = "asset bytes\n"

	sum := sha256.Sum256([]byte(body))
	wantDigest := hex.EncodeToString(sum[:])

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/repos/org/repo/releases/tags/v1.0.0-rc.1":
			if r.Header.Get("Authorization") != "token forgejo-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}

			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0-rc.1","name":"RC","body":"notes","draft":true,"prerelease":true,` +
				`"assets":[{"name":"a.txt","size":12,"browser_download_url":"https://forge.invalid/dl/a.txt"}]}`))
		case r.URL.Path == "/api/v4/projects/org%2Frepo/releases/v1.0.0" || r.URL.RawPath == "/api/v4/projects/org%2Frepo/releases/v1.0.0":
			if r.Header.Get("PRIVATE-TOKEN") != "gitlab-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}

			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","name":"One","description":"notes","assets":{"links":[{"name":"a.txt","url":"https://forge.invalid/dl/a.txt"}]}}`))
		case r.URL.Path == "/dl/a.txt":
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	})

	forgejo := rawref.Reader{Forge: rawref.Forgejo, Base: "https://forge.invalid", Token: "forgejo-token", Owner: "org", Client: inMemoryClient(handler)}

	release, found, err := rawref.ReleaseByTag(context.Background(), forgejo, "repo", "v1.0.0-rc.1")
	if err != nil || !found {
		t.Fatalf("forgejo: found=%v err=%v", found, err)
	}

	if release.Tag != "v1.0.0-rc.1" || release.Name != "RC" || release.Body != "notes" || !release.Draft || !release.Prerelease {
		t.Errorf("forgejo release = %+v, want the recorded prerelease draft", release)
	}

	if len(release.Assets) != 1 || release.Assets[0] != (rawref.Asset{Name: "a.txt", Size: 12, Digest: wantDigest}) {
		t.Errorf("forgejo assets = %+v, want a.txt with the served digest", release.Assets)
	}

	gitlab := rawref.Reader{Forge: rawref.GitLab, Base: "https://forge.invalid", Token: "gitlab-token", Owner: "org", Client: inMemoryClient(handler)}

	release, found, err = rawref.ReleaseByTag(context.Background(), gitlab, "repo", "v1.0.0")
	if err != nil || !found {
		t.Fatalf("gitlab: found=%v err=%v", found, err)
	}

	if release.Tag != "v1.0.0" || release.Name != "One" || release.Body != "notes" || release.Draft || release.Prerelease {
		t.Errorf("gitlab release = %+v, want a published, non-prerelease release", release)
	}

	if len(release.Assets) != 1 || release.Assets[0] != (rawref.Asset{Name: "a.txt", Size: int64(len(body)), Digest: wantDigest}) {
		t.Errorf("gitlab assets = %+v, want a.txt with the served size and digest", release.Assets)
	}

	// Absence is reported as not found, never as an empty release.
	if _, found, err := rawref.ReleaseByTag(context.Background(), forgejo, "repo", "v9.9.9"); err != nil || found {
		t.Errorf("missing release: found=%v err=%v", found, err)
	}

	if _, _, err := rawref.ReleaseByTag(context.Background(), rawref.Reader{Forge: "github"}, "repo", "v1"); err == nil || !strings.Contains(err.Error(), "no reader") {
		t.Errorf("unknown forge err = %v", err)
	}
}
