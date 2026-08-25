// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package selfrepo

import "testing"

// TestRepoSlugFromModulePath locks in the module-path → owner/repo
// derivation that lets a fork check its own `uses:` slug without a rebuild.
func TestRepoSlugFromModulePath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, in, want string
	}{
		{"github default", "github.com/diggsweden/reusable-ci", "diggsweden/reusable-ci"},
		{"forked org", "github.com/myagency/reusable-ci", "myagency/reusable-ci"},
		{"self-hosted forge host", "git.myagency.gov/team/reusable-ci", "team/reusable-ci"},
		{"major-version suffix dropped", "github.com/foo/bar/v2", "foo/bar"},
		{"no major suffix when not vN", "github.com/foo/bar/baz", "bar/baz"},
		{"too short → empty", "reusable-ci", ""},
		{"empty → empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := SlugFromModulePath(tc.in); got != tc.want {
				t.Errorf("SlugFromModulePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Slug must answer for the binary under test, which is what lets
// `platform resolve-ref --self` target reusable-ci from inside a
// consumer's release rather than the consumer's own remote.
func TestSlug_ResolvesThisModule(t *testing.T) {
	t.Parallel()

	if got := Slug(); got != "diggsweden/reusable-ci" {
		t.Errorf("Slug() = %q, want %q", got, "diggsweden/reusable-ci")
	}
}
