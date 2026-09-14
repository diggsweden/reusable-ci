// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// What the domain still owns is the fallback: the per-forge routing moved to the
// adapters behind provider.WebURLBuilder, and is asserted there against each
// forge's real shape.

type fakeURLs struct{}

func (fakeURLs) ReleaseWebURL(server, repo, version string) string {
	return server + "/" + repo + "/rel/" + version
}

func (fakeURLs) PackagesWebURL(server, repo string) string {
	return server + "/" + repo + "/pkg"
}

func TestReleaseURL_DelegatesToTheBuilder(t *testing.T) {
	t.Parallel()

	got := summary.ReleaseURL(fakeURLs{}, "https://forge.example", "owner/repo", "v1.0.0")
	if want := "https://forge.example/owner/repo/rel/v1.0.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPackagesURL_DelegatesToTheBuilder(t *testing.T) {
	t.Parallel()

	got := summary.PackagesURL(fakeURLs{}, "https://forge.example", "owner/repo")
	if want := "https://forge.example/owner/repo/pkg"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestURLs_NoLinkWithoutAPage pins the empty result, and the arguments reaching
// the builder. A platform with no hosted web UI passes a nil builder; a run
// missing its server, repository or version has nothing to link to either.
// Returning a placeholder here used to put text such as "(release: v1.0.0)"
// into a Markdown link destination, so the renderer now decides the fallback.
func TestURLs_NoLinkWithoutAPage(t *testing.T) {
	t.Parallel()

	for name, got := range map[string]string{
		"release, no builder":    summary.ReleaseURL(nil, "https://forge.example", "owner/repo", "v1.0.0"),
		"release, no server":     summary.ReleaseURL(fakeURLs{}, "", "owner/repo", "v1.0.0"),
		"release, no repository": summary.ReleaseURL(fakeURLs{}, "https://forge.example", "", "v1.0.0"),
		"release, no version":    summary.ReleaseURL(fakeURLs{}, "https://forge.example", "owner/repo", ""),
		"packages, no builder":   summary.PackagesURL(nil, "https://forge.example", "owner/repo"),
		"packages, no server":    summary.PackagesURL(fakeURLs{}, "", "owner/repo"),
		"packages, no repo":      summary.PackagesURL(fakeURLs{}, "https://forge.example", ""),
	} {
		if got != "" {
			t.Errorf("%s = %q, want no link", name, got)
		}
	}
}
