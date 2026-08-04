// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
)

func TestWebURLs(t *testing.T) {
	t.Parallel()

	p := github.New()

	if got := p.ReleaseWebURL("https://github.com", "owner/repo", "v1.0.0"); got != "https://github.com/owner/repo/releases/tag/v1.0.0" {
		t.Errorf("ReleaseWebURL = %q", got)
	}

	if got := p.PackagesWebURL("https://github.com", "owner/repo"); got != "https://github.com/owner/repo/packages" {
		t.Errorf("PackagesWebURL = %q", got)
	}
}
