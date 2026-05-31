// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestDeleteTag_DeletesContainerPackageVersion(t *testing.T) {
	t.Parallel()

	var deletedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletedPath = r.URL.Path

			w.WriteHeader(http.StatusNoContent)

			return
		}

		http.Error(w, "unexpected "+r.Method, http.StatusNotFound)
	}))
	defer srv.Close()

	p := &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}

	// A fully-qualified ref; the package API targets owner/name/version,
	// not the ref's host.
	if err := p.DeleteTag(context.Background(), "codeberg.org/itiquette/gommitlint:staging-v1.2.3"); err != nil {
		t.Fatalf("DeleteTag = %v", err)
	}

	const want = "/api/v1/packages/itiquette/container/gommitlint/staging-v1.2.3"
	if deletedPath != want {
		t.Errorf("DELETE path = %q, want %q", deletedPath, want)
	}
}

func TestDeleteTag_RejectsRefWithoutTag(t *testing.T) {
	t.Parallel()

	// A digest-pinned ref (no tag) cannot be untagged.
	err := forgejo.New().DeleteTag(context.Background(), "codeberg.org/itiquette/gommitlint@sha256:"+
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if !errors.Is(err, errs.ErrUsage) {
		t.Errorf("ref without a tag should be a usage error, got %v", err)
	}
}
