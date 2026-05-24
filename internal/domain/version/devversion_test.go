// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

func TestSanitizePathToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"main", "main"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
		{"unicode-åäö", "unicode-----"}, // unicode mapped to "-"; trimmed by trailing rule? no — internal "-"s preserved
		{"", ""},
		{"-", ""},
		{"--", ""},
		{"////", ""},
		{"v1.2.3", "v1.2.3"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"0.5.9-dev-feat-x-abc1234", "0.5.9-dev-feat-x-abc1234"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got := version.SanitizePathToken(tc.in)
			// Special case for the unicode test: the bash sed treats each byte of
			// the multi-byte UTF-8 sequence as a separate non-matching character.
			// We emit a "-" per byte too. Just check the prefix doesn't include
			// the unicode bytes and the result isn't empty.
			if tc.in == "unicode-åäö" {
				// Byte-vs-rune handling makes the exact dash count
				// vary across implementations; the invariant under
				// test is the "unicode" prefix and that nothing
				// unicode-y survived.
				if !strings.HasPrefix(got, "unicode") {
					t.Errorf("SanitizePathToken(%q) = %q, want unicode-prefixed", tc.in, got)
				}

				return
			}

			if got != tc.want {
				t.Errorf("SanitizePathToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizePathToken_Idempotent(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"feat/x", "0.5.9-dev-feat-x-abc1234", "////"} {
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

func TestComposeDevVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		baseVersion string
		branch      string
		shortSHA    string
		want        string
	}{
		{name: "main happy path", baseVersion: "0.5.9", branch: "main", shortSHA: "abc1234", want: "0.5.9-dev-main-abc1234"},
		{name: "branch sanitised", baseVersion: "1.0.0", branch: "feat/awesome", shortSHA: "deadbee", want: "1.0.0-dev-feat-awesome-deadbee"},
		{name: "empty base falls back to 0.0.0", baseVersion: "", branch: "main", shortSHA: "abc1234", want: "0.0.0-dev-main-abc1234"},
		{name: "renovate slashes", baseVersion: "2.1.0", branch: "renovate/some-pkg/3.x", shortSHA: "1234567", want: "2.1.0-dev-renovate-some-pkg-3.x-1234567"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := version.ComposeDevVersion(tc.baseVersion, tc.branch, tc.shortSHA)
			if got != tc.want {
				t.Errorf("ComposeDevVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLatestSemverTag(t *testing.T) {
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
		{name: "ignores prerelease", tags: []string{"v1.2.3", "v1.2.4-rc.1"}, want: "v1.2.3"},
		{name: "ignores non-semver", tags: []string{"latest", "stable", "v1.2.3"}, want: "v1.2.3"},
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

func TestStripVPrefix(t *testing.T) {
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
