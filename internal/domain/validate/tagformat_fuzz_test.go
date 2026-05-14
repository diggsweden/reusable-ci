// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func FuzzParseTagFormat(f *testing.F) {
	seeds := []string{
		"",
		"v1.0.0",
		"v0.0.1",
		"v10.20.30",
		"v1.0.0-alpha",
		"v1.0.0-beta.1",
		"v1.0.0-rc.2",
		"v1.0.0-SNAPSHOT",
		"v1.0.0+build.123",
		"v1.0.0-rc.1+sha.abc123",
		"v1.0.0-custom.123",
		"1.0.0",
		"V1.0.0",
		"v01.0.0",
		"v1.0.0-rc.01",
		"foobar",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, tag string) {
		tf, err := validate.ParseTagFormat(tag)
		if err != nil {
			return
		}

		if tf == nil {
			t.Fatalf("nil TagFormat for valid tag %q", tag)
		}
		if tf.Tag != tag {
			t.Fatalf("Tag = %q, want %q", tf.Tag, tag)
		}
		if tf.Major == "" || tf.Minor == "" || tf.Patch == "" {
			t.Fatalf("missing version parts for %q: %+v", tag, tf)
		}
		if tf.IsStable() != (tf.Prerelease == "") {
			t.Fatalf("IsStable inconsistent for %q: prerelease=%q", tag, tf.Prerelease)
		}
		if tf.Prerelease == "" && !tf.PrereleaseStandard {
			t.Fatalf("stable tag should always be standard: %+v", tf)
		}

		reformatted := "v" + tf.Major + "." + tf.Minor + "." + tf.Patch
		if tf.Prerelease != "" {
			reformatted += "-" + tf.Prerelease
		}
		if tf.Build != "" {
			reformatted += "+" + tf.Build
		}

		reparsed, err := validate.ParseTagFormat(reformatted)
		if err != nil {
			t.Fatalf("reparse of %q failed: %v", reformatted, err)
		}
		if reparsed.Major != tf.Major || reparsed.Minor != tf.Minor || reparsed.Patch != tf.Patch {
			t.Fatalf("reparse changed version parts: before=%+v after=%+v", tf, reparsed)
		}
		if reparsed.Prerelease != tf.Prerelease || reparsed.Build != tf.Build {
			t.Fatalf("reparse changed prerelease/build: before=%+v after=%+v", tf, reparsed)
		}
	})
}
