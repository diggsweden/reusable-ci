// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
)

func TestListContainerPackageVersions_FiltersContainerPackageVersions(t *testing.T) {
	t.Parallel()

	var (
		gotPath  string
		gotQuery string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
{"type":"container","name":"nanolinter-base","version":"staging-a-rust"},
{"type":"container","name":"nanolinter-base","version":"1.0.0"},
{"type":"generic","name":"nanolinter-base","version":"ignored"},
{"type":"container","name":"other","version":"ignored"}
]`))
	}))
	defer srv.Close()

	p := &forgejo.Provider{
		Env:             envMap(map[string]string{"FORGEJO_TOKEN": "tok"}),
		HTTPClient:      srv.Client(),
		APIBaseOverride: srv.URL,
	}

	got, err := p.ListContainerPackageVersions(context.Background(), "itiquette", "nanolinter-base")
	if err != nil {
		t.Fatal(err)
	}

	if want := "/api/v1/packages/itiquette/container/nanolinter-base"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}

	if gotQuery == "" {
		t.Fatal("query is empty, want pagination query")
	}

	if want := []string{"staging-a-rust", "1.0.0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
}
