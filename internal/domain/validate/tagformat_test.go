// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestParseTagFormat_EmptyInputUsage(t *testing.T) {
	t.Parallel()

	_, err := validate.ParseTagFormat("")
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v", err)
	}
}

func TestParseTagFormat_StableReleases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag   string
		major string
		minor string
		patch string
	}{
		{"v1.0.0", "1", "0", "0"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"v0.0.1", "0", "0", "1"},
		{"v10.20.30", "10", "20", "30"},
		{"v0.1.0", "0", "1", "0"},
		{"v100.200.300", "100", "200", "300"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		tf, err := validate.ParseTagFormat(c.tag)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.tag, err)

			continue
		}

		if !tf.IsStable() {
			t.Errorf("%s: should be stable, got prerelease %q", c.tag, tf.Prerelease)
		}

		if tf.Major != c.major || tf.Minor != c.minor || tf.Patch != c.patch {
			t.Errorf("%s: %s.%s.%s, want %s.%s.%s",
				c.tag, tf.Major, tf.Minor, tf.Patch, c.major, c.minor, c.patch)
		}
	}
}

func TestParseTagFormat_StandardPrereleases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag        string
		prerelease string
	}{
		{"v2.3.4-beta.1", "beta.1"},
		{"v1.0.0-alpha", "alpha"},
		{"v1.0.0-rc.2", "rc.2"},
		{"v1.0.0-SNAPSHOT", "SNAPSHOT"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"v1.0.0-dev", "dev"},
		{"v1.0.0-alpha.1", "alpha.1"},
		{"v1.0.0-beta", "beta"},
		{"v1.0.0-rc.10", "rc.10"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		tf, err := validate.ParseTagFormat(c.tag)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.tag, err)

			continue
		}

		if tf.Prerelease != c.prerelease {
			t.Errorf("%s: prerelease %q, want %q", c.tag, tf.Prerelease, c.prerelease)
		}

		if !tf.PrereleaseStandard {
			t.Errorf("%s: PrereleaseStandard should be true for %q", c.tag, c.prerelease)
		}

		if tf.IsStable() {
			t.Errorf("%s: should NOT be stable", c.tag)
		}
	}
}

func TestParseTagFormat_BuildMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag        string
		prerelease string
		build      string
	}{
		{"v1.0.0+build.123", "", "build.123"},
		{"v1.0.0-rc.1+sha.abc123", "rc.1", "sha.abc123"},
		{"v2.3.4+meta-only", "", "meta-only"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		tf, err := validate.ParseTagFormat(c.tag)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.tag, err)

			continue
		}

		if tf.Prerelease != c.prerelease {
			t.Errorf("%s: prerelease %q, want %q", c.tag, tf.Prerelease, c.prerelease)
		}

		if tf.Build != c.build {
			t.Errorf("%s: build %q, want %q", c.tag, tf.Build, c.build)
		}
	}
}

func TestParseTagFormat_NonStandardPrereleasesAcceptedButFlagged(t *testing.T) {
	t.Parallel()

	cases := []string{
		"v1.0.0-custom.123",
		"v1.0.0-preview.1",
		"v1.0.0-foo",
	}
	for _, tag := range cases {
		tf, err := validate.ParseTagFormat(tag)
		if err != nil {
			t.Errorf("%s: should be accepted, got %v", tag, err)

			continue
		}

		if tf.PrereleaseStandard {
			t.Errorf("%s: PrereleaseStandard should be false", tag)
		}
	}
}

func TestParseTagFormat_RejectsBadTags(t *testing.T) {
	t.Parallel()

	cases := []string{
		"1.0.0",    // missing v prefix
		"V1.0.0",   // uppercase V
		"v1.0",     // incomplete
		"v1",       // single digit
		"v1.0.0.0", // four parts
		"vX.Y.Z",   // non-numeric
		"v1a.0.0",  // mixed alphanumeric major
		// Leading zeros are invalid per official SemVer 2.0.0 — the regex
		// catches them at the CI gate. The bash heritage was permissive
		// here; we deliberately tightened.
		"v01.0.0",
		"v1.01.0",
		"v1.0.01",
		// Pre-release with leading-zero numeric identifier is also invalid.
		"v1.0.0-rc.01",
		"release-1.0.0",
		"version-1.0.0",
		"foobar",
		"abc123def",
	}
	for _, tag := range cases {
		_, err := validate.ParseTagFormat(tag)
		if err == nil {
			t.Errorf("%s: expected rejection", tag)

			continue
		}

		if !strings.Contains(err.Error(), "invalid tag format") {
			t.Errorf("%s: wrong error %v", tag, err)
		}
	}
}
