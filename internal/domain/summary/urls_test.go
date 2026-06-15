// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/summary"
)

func TestReleaseURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		plat provider.Platform
		want string
	}{
		{provider.PlatformGitHub, "https://github.com/owner/repo/releases/tag/v1.0.0"},
		{provider.PlatformGitLab, "https://gitlab.com/owner/repo/-/releases/v1.0.0"},
		{provider.PlatformLocal, "(release: v1.0.0)"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		var server string

		switch c.plat {
		case provider.PlatformGitHub:
			server = "https://github.com"
		case provider.PlatformGitLab:
			server = "https://gitlab.com"
		case provider.PlatformForgejo:
			// Forgejo summary URLs are wired in a later phase (the
			// forge adapter); no case is exercised here yet.
		case provider.PlatformLocal:
			// No server URL needed — the local placeholder ignores it.
		}

		got := summary.ReleaseURL(c.plat, server, "owner/repo", "v1.0.0")
		if got != c.want {
			t.Errorf("plat=%s got %q, want %q", c.plat, got, c.want)
		}
	}
}

func TestPackagesURL(t *testing.T) {
	t.Parallel()

	if got := summary.PackagesURL(provider.PlatformGitHub, "https://github.com", "owner/repo"); got != "https://github.com/owner/repo/packages" {
		t.Errorf("github = %q", got)
	}

	if got := summary.PackagesURL(provider.PlatformGitLab, "https://gitlab.com", "owner/repo"); got != "https://gitlab.com/owner/repo/-/packages" {
		t.Errorf("gitlab = %q", got)
	}

	if got := summary.PackagesURL(provider.PlatformLocal, "", ""); got != "(packages)" {
		t.Errorf("local = %q", got)
	}
}
