// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
)

func TestWebURLs(t *testing.T) {
	t.Parallel()

	p := gitlab.New()

	if got := p.ReleaseWebURL("https://gitlab.com", "owner/repo", "v1.0.0"); got != "https://gitlab.com/owner/repo/-/releases/v1.0.0" {
		t.Errorf("ReleaseWebURL = %q", got)
	}

	if got := p.PackagesWebURL("https://gitlab.com", "owner/repo"); got != "https://gitlab.com/owner/repo/-/packages" {
		t.Errorf("PackagesWebURL = %q", got)
	}
}
