// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"net/url"
	"testing"
)

func TestGitHubUploadURL_UsesEnterpriseUploadsEndpoint(t *testing.T) {
	t.Parallel()

	api, err := url.Parse("https://github.example.test/api/v3/")
	if err != nil {
		t.Fatal(err)
	}

	if got := githubUploadURL(api).String(); got != "https://github.example.test/api/uploads/" {
		t.Fatalf("upload URL = %q", got)
	}

	if got := api.String(); got != "https://github.example.test/api/v3/" {
		t.Fatalf("API URL was mutated: %q", got)
	}
}

func TestGitHubUploadURL_TestOverrideKeepsSharedBase(t *testing.T) {
	t.Parallel()

	api, err := url.Parse("https://fixture.example.test/")
	if err != nil {
		t.Fatal(err)
	}

	if got := githubUploadURL(api).String(); got != api.String() {
		t.Fatalf("upload URL = %q, want %q", got, api)
	}
}
