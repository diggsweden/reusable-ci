// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
)

func TestWebURLs(t *testing.T) {
	t.Parallel()

	p := forgejo.New()

	if got := p.ReleaseWebURL("https://codeberg.org", "owner/repo", "v1.0.0"); got != "https://codeberg.org/owner/repo/releases/tag/v1.0.0" {
		t.Errorf("ReleaseWebURL = %q", got)
	}

	// Forgejo packages hang off the owner, not the repository.
	if got := p.PackagesWebURL("https://codeberg.org", "owner/repo"); got != "https://codeberg.org/owner/-/packages" {
		t.Errorf("PackagesWebURL = %q", got)
	}
}
