// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor

import "testing"

// TestRepoSlugFromModulePath_DropsTheHostAndMajorVersionSuffix locks in the module-path → owner/repo
// derivation that lets a fork check its own `uses:` slug without a rebuild.
func TestRepoSlugFromModulePath_DropsTheHostAndMajorVersionSuffix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, in, want string
	}{
		{"github default", "github.com/diggsweden/reusable-ci", "diggsweden/reusable-ci"},
		{"forked org", "github.com/myagency/reusable-ci", "myagency/reusable-ci"},
		{"self-hosted forge host", "git.myagency.gov/team/reusable-ci", "team/reusable-ci"},
		{"major-version suffix dropped", "github.com/foo/bar/v2", "foo/bar"},
		{"v3 suffix", "github.com/foo/bar/v3", "foo/bar"},
		{"multi-digit suffix", "github.com/foo/bar/v10", "foo/bar"},
		{"v0 is invalid", "github.com/foo/bar/v0", ""},
		{"v1 is invalid", "github.com/foo/bar/v1", ""},
		{"leading zero is invalid", "github.com/foo/bar/v02", ""},
		{"nondigits are not a major-version suffix", "github.com/foo/bar/vNext", "bar/vNext"},
		{"no major suffix when not vN", "github.com/foo/bar/baz", "bar/baz"},
		{"too short → empty", "reusable-ci", ""},
		{"empty → empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := repoSlugFromModulePath(tc.in); got != tc.want {
				t.Errorf("repoSlugFromModulePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
