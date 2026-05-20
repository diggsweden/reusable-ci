// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestCargoTargetTriple(t *testing.T) {
	t.Parallel()

	cases := []struct {
		platform string
		triple   string
	}{
		{"linux/amd64", "x86_64-unknown-linux-gnu"},
		{"linux/arm64", "aarch64-unknown-linux-gnu"},
		{"darwin/arm64", "aarch64-apple-darwin"},
	}
	for _, tc := range cases {
		if got := build.CargoTargetTriple(tc.platform); got != tc.triple {
			t.Errorf("CargoTargetTriple(%q) = %q, want %q", tc.platform, got, tc.triple)
		}

		if !build.IsKnownCargoPlatform(tc.platform) {
			t.Errorf("IsKnownCargoPlatform(%q) = false, want true", tc.platform)
		}
	}

	if build.IsKnownCargoPlatform("plan9/amd64") {
		t.Errorf("plan9/amd64 should not be a known cargo platform")
	}
}
