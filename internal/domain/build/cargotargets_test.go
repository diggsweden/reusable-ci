// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"maps"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestCargoTargetTriple_MapsPlatformsToRustTriples(t *testing.T) {
	t.Parallel()

	// Every entry in the canonical table, not a sample: a triple that drifted
	// would silently cross-compile for the wrong architecture, and the table
	// is the only place that says which spelling maps to which target.
	want := map[string]string{
		"linux/amd64":   "x86_64-unknown-linux-gnu",
		"linux/arm64":   "aarch64-unknown-linux-gnu",
		"linux/arm":     "armv7-unknown-linux-gnueabihf",
		"darwin/amd64":  "x86_64-apple-darwin",
		"darwin/arm64":  "aarch64-apple-darwin",
		"windows/amd64": "x86_64-pc-windows-gnu",
	}
	if !maps.Equal(build.CargoTargetTriples, want) {
		t.Errorf("CargoTargetTriples = %v, want %v", build.CargoTargetTriples, want)
	}

	for platform, triple := range want {
		if got := build.CargoTargetTriple(platform); got != triple {
			t.Errorf("CargoTargetTriple(%q) = %q, want %q", platform, got, triple)
		}

		if !build.IsKnownCargoPlatform(platform) {
			t.Errorf("IsKnownCargoPlatform(%q) = false, want true", platform)
		}
	}

	// An unknown platform reports false AND yields the empty triple. Checking
	// only the boolean left the caller's other question unanswered: a lookup
	// that returned some default would have passed.
	const unknown = "plan9/amd64"
	if build.IsKnownCargoPlatform(unknown) {
		t.Errorf("IsKnownCargoPlatform(%q) = true, want false", unknown)
	}

	if got := build.CargoTargetTriple(unknown); got != "" {
		t.Errorf("CargoTargetTriple(%q) = %q, want the empty triple", unknown, got)
	}
}
