// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestSplitPlatform_SplitsOSAndArchAndRejectsMalformed(t *testing.T) {
	t.Parallel()

	// Several distinct pairs, not one. With a single fixture a SplitPlatform
	// that returned ("linux", "arm64") unconditionally passed, and so did one
	// that swapped the two components for a symmetric-looking input.
	for _, tc := range []struct{ in, goos, goarch string }{
		{in: " linux / arm64 ", goos: "linux", goarch: "arm64"},
		{in: "darwin/amd64", goos: "darwin", goarch: "amd64"},
		{in: "windows /386", goos: "windows", goarch: "386"},
	} {
		goos, goarch, err := build.SplitPlatform(tc.in)
		if err != nil {
			t.Fatalf("SplitPlatform(%q): unexpected error: %v", tc.in, err)
		}

		if goos != tc.goos || goarch != tc.goarch {
			t.Errorf("SplitPlatform(%q) = (%q, %q), want (%q, %q)", tc.in, goos, goarch, tc.goos, tc.goarch)
		}
	}

	// A malformed --platforms entry is a bad flag value, so it must classify
	// as ErrUsage (exit 2) rather than falling through to the unclassified 70.
	for _, bad := range []string{"", "linux", "linux/", "/arm64", "linux/amd64/extra", "  /  "} {
		if _, _, err := build.SplitPlatform(bad); !errors.Is(err, errs.ErrUsage) {
			t.Errorf("SplitPlatform(%q) err = %v, want ErrUsage", bad, err)
		}
	}
}
