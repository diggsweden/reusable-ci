// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"regexp"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestAnchorIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repoURL string
		want    string
	}{
		{"github repo", "https://github.com/acme/app", `^https://github\.com/acme/app/`},
		{"gitlab repo", "https://gitlab.com/grp/sub/proj", `^https://gitlab\.com/grp/sub/proj/`},
		{"trailing slash trimmed", "https://github.com/acme/app/", `^https://github\.com/acme/app/`},
		{"surrounding whitespace trimmed", "  https://github.com/acme/app  ", `^https://github\.com/acme/app/`},
		{"empty fails closed", "", "^$"},
		{"whitespace-only fails closed", "   ", "^$"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := provider.AnchorIdentity(provider.AttestedRepoURL(tc.repoURL)); got != tc.want {
				t.Errorf("AnchorIdentity(%q) = %q, want %q", tc.repoURL, got, tc.want)
			}
		})
	}
}

// TestAnchorIdentity_NoSiblingRepoMatch is the security regression guard: the
// anchored regexp for one repository must not accept a SAN from a
// sibling/prefix repository (…/acme/app must reject …/acme/app-evil), and the
// '.' in the host must not act as a wildcard.
func TestAnchorIdentity_NoSiblingRepoMatch(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(provider.AnchorIdentity(provider.AttestedRepoURL("https://github.com/acme/app")))

	accept := []string{
		"https://github.com/acme/app/.github/workflows/release.yml@refs/tags/v1.2.3",
		"https://github.com/acme/app/.gitea/workflows/ci.yml@refs/heads/main",
	}
	for _, san := range accept {
		if !re.MatchString(san) {
			t.Errorf("identity regexp should ACCEPT same-repo SAN %q", san)
		}
	}

	reject := []string{
		"https://github.com/acme/app-evil/.github/workflows/release.yml@refs/tags/v1",
		"https://github.com/acmexapp/.github/workflows/release.yml@refs/tags/v1",
		"https://githubXcom/acme/app/.github/workflows/release.yml@refs/tags/v1",
		"https://evil.com/acme/app/.github/workflows/release.yml@refs/tags/v1",
		"prefixed-https://github.com/acme/app/x",
	}
	for _, san := range reject {
		if re.MatchString(san) {
			t.Errorf("identity regexp should REJECT non-matching SAN %q", san)
		}
	}
}
