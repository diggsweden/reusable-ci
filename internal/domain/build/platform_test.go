// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestSplitPlatform(t *testing.T) {
	t.Parallel()

	goos, goarch, err := build.SplitPlatform(" linux / arm64 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if goos != "linux" || goarch != "arm64" {
		t.Errorf("got (%q, %q), want (linux, arm64)", goos, goarch)
	}

	for _, bad := range []string{"", "linux", "linux/", "/arm64", "linux/amd64/extra", "  /  "} {
		if _, _, err := build.SplitPlatform(bad); err == nil {
			t.Errorf("SplitPlatform(%q) should error", bad)
		}
	}
}
