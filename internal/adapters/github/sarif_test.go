// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// UploadSARIF was uncovered. It is how scanner findings reach a
// repository's security tab, and the encoding is easy to get wrong in a
// way nothing local would notice: the API takes base64(gzip(sarif)), so
// sending plain base64 or raw JSON produces an upload that is accepted
// shape-wise and carries no findings.

type sarifRequest struct {
	path   string
	auth   string
	accept string
	body   map[string]string
}

func newSARIFServer(t *testing.T, rec *sarifRequest, status int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.auth = r.Header.Get("Authorization")
		rec.accept = r.Header.Get("Accept")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &rec.body)

		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"id":"1"}`)
	}))

	t.Cleanup(srv.Close)

	return srv
}

// decodeSARIF reverses what the uploader is supposed to have done. If
// either step is missing on the product side this fails, which is the
// point: the encoding cannot be checked by looking at the string.
func decodeSARIF(t *testing.T, encoded string) []byte {
	t.Helper()

	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("payload is not gzip inside base64 -- the API would reject or silently ingest nothing: %v", err)
	}

	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

func TestUploadSARIF_SendsGzippedBase64(t *testing.T) {
	t.Parallel()

	const sarif = `{"version":"2.1.0","runs":[{"automationDetails":{"id":"gosec/"}}]}`

	var rec sarifRequest

	srv := newSARIFServer(t, &rec, http.StatusAccepted)

	provider := &github.Provider{HTTPClient: srv.Client(), APIBaseOverride: srv.URL}

	err := provider.UploadSARIF(context.Background(), providerSARIF(sarif, "tok", "owner/repo"))
	if err != nil {
		t.Fatalf("UploadSARIF: %v", err)
	}

	if rec.path != "/repos/owner/repo/code-scanning/sarifs" {
		t.Errorf("path = %q", rec.path)
	}

	if rec.auth != "token tok" {
		t.Errorf("Authorization = %q, want the token", rec.auth)
	}

	if got := string(decodeSARIF(t, rec.body["sarif"])); got != sarif {
		t.Errorf("decoded SARIF = %q, want %q", got, sarif)
	}

	// The commit and ref decide which code the findings are attached to.
	// Attaching them to the wrong ref shows a clean branch as vulnerable
	// or hides findings on the one being released.
	if rec.body["commit_sha"] != "deadbeef" || rec.body["ref"] != "refs/heads/main" {
		t.Errorf("payload identity = (%q, %q), want (deadbeef, refs/heads/main)",
			rec.body["commit_sha"], rec.body["ref"])
	}
}

// TestUploadSARIF_PayloadIsNotPlainBase64 states the encoding claim from
// the other side, so a reader can see what the round-trip above is for.
func TestUploadSARIF_PayloadIsNotPlainBase64(t *testing.T) {
	t.Parallel()

	const sarif = `{"version":"2.1.0","runs":[]}`

	var rec sarifRequest

	srv := newSARIFServer(t, &rec, http.StatusAccepted)

	provider := &github.Provider{HTTPClient: srv.Client(), APIBaseOverride: srv.URL}
	if err := provider.UploadSARIF(context.Background(), providerSARIF(sarif, "tok", "owner/repo")); err != nil {
		t.Fatal(err)
	}

	decoded, err := base64.StdEncoding.DecodeString(rec.body["sarif"])
	if err != nil {
		t.Fatalf("payload is not base64 at all: %v", err)
	}

	if string(decoded) == sarif {
		t.Error("the SARIF was base64'd without being gzipped first")
	}

	// gzip's magic bytes: the compressed stream has to be what base64
	// wraps, not JSON that merely differs from the input.
	if len(decoded) < 2 || decoded[0] != 0x1f || decoded[1] != 0x8b {
		t.Errorf("base64 does not wrap a gzip stream, first bytes %x", decoded[:min(2, len(decoded))])
	}
}

func TestUploadSARIF_Refusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		token      string
		repository string
		want       error
	}{
		{
			// Without a token the POST would go out unauthenticated and
			// be rejected by the forge; refusing here names the cause.
			name: "no token", token: "", repository: "owner/repo", want: errs.ErrPermissionDenied,
		},
		{
			name: "no repository", token: "tok", repository: "", want: errs.ErrUsage,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rec sarifRequest

			srv := newSARIFServer(t, &rec, http.StatusAccepted)

			provider := &github.Provider{HTTPClient: srv.Client(), APIBaseOverride: srv.URL}

			err := provider.UploadSARIF(context.Background(),
				providerSARIF(`{"version":"2.1.0"}`, tc.token, tc.repository))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			if rec.path != "" {
				t.Errorf("a request was sent anyway, to %q", rec.path)
			}
		})
	}
}

// TestUploadSARIF_ServerRejectionIsAnError keeps a refused upload from
// reading as a successful one: a scan whose findings never landed must
// not report that they did.
func TestUploadSARIF_ServerRejectionIsAnError(t *testing.T) {
	t.Parallel()

	var rec sarifRequest

	srv := newSARIFServer(t, &rec, http.StatusForbidden)

	provider := &github.Provider{HTTPClient: srv.Client(), APIBaseOverride: srv.URL}

	err := provider.UploadSARIF(context.Background(), providerSARIF(`{"version":"2.1.0"}`, "tok", "owner/repo"))
	if err == nil {
		t.Fatal("a 403 from the code-scanning API was reported as a successful upload")
	}

	if !strings.Contains(strings.ToLower(err.Error()), "403") &&
		!strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		t.Errorf("the error does not carry the status the forge returned: %v", err)
	}
}

func providerSARIF(sarif, token, repository string) provider.SARIFUpload {
	return provider.SARIFUpload{
		SARIF:      []byte(sarif),
		Token:      token,
		Repository: repository,
		SHA:        "deadbeef",
		Ref:        "refs/heads/main",
	}
}
