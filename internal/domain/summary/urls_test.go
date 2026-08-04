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

// A platform with no hosted web UI does not implement the role, and the summary
// must still render something a reader can parse rather than an empty cell.
func TestURLs_PlaceholderWhenThePlatformHasNoWebUI(t *testing.T) {
	t.Parallel()

	if got := summary.ReleaseURL(nil, "", "owner/repo", "v1.0.0"); got != "(release: v1.0.0)" {
		t.Errorf("release placeholder = %q", got)
	}

	if got := summary.PackagesURL(nil, "", "owner/repo"); got != "(packages)" {
		t.Errorf("packages placeholder = %q", got)
	}
}
