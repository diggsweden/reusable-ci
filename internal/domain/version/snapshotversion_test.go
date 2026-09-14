// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

func TestSanitizePathToken_ReplacesPathSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"main", "main"},                 //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"feat/awesome", "feat-awesome"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"feat/awesome/sub", "feat-awesome-sub"},
		{"release/2026.05", "release-2026.05"},
		{"42/merge", "42-merge"},
		{"renovate/branch with spaces", "renovate-branch-with-spaces"},
		{"Feat/X", "Feat-X"},
		{"-leading", "leading"},
		{"trailing-", "trailing"},
		{"---multi---dashes---", "multi---dashes"},
		{"a//b//c", "a--b--c"},
		{"///foo", "foo"},
		{"keeps.dots_and_underscores", "keeps.dots_and_underscores"},
		// Non-ASCII is replaced one dash per *byte*, then the ordinary
		// trim/collapse rules apply -- so trailing non-ASCII disappears
		// entirely while internal non-ASCII survives as dashes.
		{"unicode-åäö", "unicode"},
		{"a-å-b", "a----b"},
		{"åäö", ""},
		{"", ""},
		{"-", ""},
		{"--", ""},
		{"////", ""},
		{".", ""},
		{"..", ""},
		{"-..-", ""},
		{"v1.2.3", "v1.2.3"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"0.5.9-snapshot-feat-x-abc1234", "0.5.9-snapshot-feat-x-abc1234"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got := version.SanitizePathToken(tc.in)
			if got != tc.want {
				t.Errorf("SanitizePathToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizePathToken_Idempotent(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"feat/x", "0.5.9-snapshot-feat-x-abc1234", "////"} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			first := version.SanitizePathToken(in)

			second := version.SanitizePathToken(first)
			if first != second {
				t.Errorf("SanitizePathToken not idempotent for %q: first=%q second=%q", in, first, second)
			}
		})
	}
}

func TestComposeSnapshotVersion_CombinesBaseBranchAndSHA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		baseVersion string
		branch      string
		shortSHA    string
		want        string
	}{
		{name: "main happy path", baseVersion: "0.5.9", branch: "main", shortSHA: "abc1234", want: "0.5.9-snapshot-main-abc1234"},
		{name: "branch sanitised", baseVersion: "1.0.0", branch: "feat/awesome", shortSHA: "deadbee", want: "1.0.0-snapshot-feat-awesome-deadbee"},
		{name: "empty base falls back to 0.0.0", baseVersion: "", branch: "main", shortSHA: "abc1234", want: "0.0.0-snapshot-main-abc1234"},
		{name: "renovate slashes", baseVersion: "2.1.0", branch: "renovate/some-pkg/3.x", shortSHA: "1234567", want: "2.1.0-snapshot-renovate-some-pkg-3.x-1234567"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := version.ComposeSnapshotVersion(tc.baseVersion, tc.branch, tc.shortSHA)
			if got != tc.want {
				t.Errorf("ComposeSnapshotVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLatestSemverTag_PicksTheHighestVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want string
	}{
		{name: "empty list", tags: nil, want: ""},
		{name: "single tag", tags: []string{"v1.2.3"}, want: "v1.2.3"},
		{name: "picks highest", tags: []string{"v1.0.0", "v1.2.3", "v1.0.5"}, want: "v1.2.3"},
		{name: "compares numerically not lexically", tags: []string{"v9.0.0", "v10.0.0"}, want: "v10.0.0"},
		{
			name: "compares components larger than an integer",
			tags: []string{"v99999999999999999999.0.0", "v100000000000000000000.0.0"},
			want: "v100000000000000000000.0.0",
		},
		{name: "ignores prerelease", tags: []string{"v1.2.3", "v1.2.4-rc.1"}, want: "v1.2.3"},
		{name: "ignores non-semver", tags: []string{"latest", "stable", "v1.2.3"}, want: "v1.2.3"},
		{name: "ignores leading zeroes", tags: []string{"v01.2.3", "v1.2.3"}, want: "v1.2.3"},
		{name: "all non-semver", tags: []string{"latest", "main"}, want: ""},
		{name: "matches strict v-prefix", tags: []string{"1.2.3", "v1.2.3"}, want: "v1.2.3"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := version.LatestSemverTag(tc.tags); got != tc.want {
				t.Errorf("LatestSemverTag = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsStableSemverTag_RequiresAVPrefixAndNoPrerelease(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"v1.2.3":       true,
		"v0.0.0":       true,
		"v01.2.3":      false,
		"v1.02.3":      false,
		"v1.2.03":      false,
		"1.2.3":        false,
		"v1.2":         false,
		"v1.2.3-rc1":   false,
		"v1.2.3+build": false,
		"latest":       false,
	}
	for tag, want := range tests {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()

			if got := version.IsStableSemverTag(tag); got != want {
				t.Errorf("IsStableSemverTag(%q) = %v, want %v", tag, got, want)
			}
		})
	}
}

func TestParseSemver_UsesStrictThreeComponentGrammar(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"1.2.3", "v1.2.3", "v1.2.3-rc.1+build.5"} {
		parsed, ok := version.ParseSemver(value)
		if !ok {
			t.Errorf("ParseSemver(%q) rejected a valid version", value)

			continue
		}

		if parsed.Major != "1" || parsed.Minor != "2" || parsed.Patch != "3" {
			t.Errorf("ParseSemver(%q) components = %+v", value, parsed)
		}
	}

	for _, value := range []string{"v1", "v1.2", "v01.2.3", "V1.2.3", "v1.2.3-rc.01"} {
		if _, ok := version.ParseSemver(value); ok {
			t.Errorf("ParseSemver(%q) accepted a non-strict version", value)
		}
	}
}

func TestStripVPrefix_RemovesOnlyALeadingV(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"v1.2.3": "1.2.3",
		"1.2.3":  "1.2.3",
		"":       "",
		"v":      "",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			if got := version.StripVPrefix(in); got != want {
				t.Errorf("StripVPrefix(%q) = %q, want %q", in, got, want)
			}
		})
	}
}
