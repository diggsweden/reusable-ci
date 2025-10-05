// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type deleteRequestTransport func(*http.Request) (*http.Response, error)

func (f deleteRequestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

//nolint:paralleltest // replaces and restores the process-local library transport.
func TestMergeManifest_RejectsLateTagsBeforeTransport(t *testing.T) {
	original := remote.DefaultTransport

	t.Cleanup(func() { remote.DefaultTransport = original })

	calls := 0
	remote.DefaultTransport = deleteRequestTransport(func(req *http.Request) (*http.Response, error) {
		calls++

		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("fixture refusal")), Request: req}, nil
	})

	auth := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(auth, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, later := range []string{"registry.example/owner/app:bad tag", "registry.example/other/app:v2", "registry.example/owner/app:v1"} {
		err := WithAuthFile(auth).MergeManifest(t.Context(), "registry.example/owner/app", []string{strings.Repeat("a", 64)}, []string{"registry.example/owner/app:v1", later})
		if !errors.Is(err, errs.ErrValidation) || calls != 0 {
			t.Errorf("err=%v calls=%d, want validation before transport", err, calls)
		}
	}
}

//nolint:paralleltest // replaces and restores the process-local library transport.
func TestDeleteTag_KeepsTagIdentifierWhenASiblingAppears(t *testing.T) {
	// Serial: remote's default transport is replaced only for this test and
	// restored before any parallel tests run. No listener or socket is used.
	original := remote.DefaultTransport

	t.Cleanup(func() { remote.DefaultTransport = original })

	sibling, deleted := false, false
	remote.DefaultTransport = deleteRequestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "registry.example" {
			return nil, fmt.Errorf("unexpected host %q: %w", r.URL.Host, errs.ErrValidation)
		}

		header := make(http.Header)
		status, body := http.StatusOK, ""

		switch r.Method + " " + r.URL.Path {
		case "GET /v2/":
		case "HEAD /v2/owner/app/manifests/prune":
			header.Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
			header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			header.Set("Content-Length", "1")
		case "GET /v2/owner/app/tags/list":
			body = `{"name":"owner/app","tags":["prune"]}`
			sibling = true // publication after the listing snapshot
		case "DELETE /v2/owner/app/manifests/prune":
			if !sibling {
				return nil, fmt.Errorf("delete preceded the listing: %w", errs.ErrValidation)
			}

			deleted, status = true, http.StatusAccepted
		default:
			return nil, fmt.Errorf("unexpected registry request: %s %s: %w", r.Method, r.URL.Path, errs.ErrValidation)
		}

		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})

	auth := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(auth, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WithAuthFile(auth).DeleteTag(context.Background(), "registry.example/owner/app:prune"); err != nil {
		t.Fatal(err)
	}

	if !deleted || !sibling {
		t.Fatal("named-tag deletion was not exercised")
	}
}
