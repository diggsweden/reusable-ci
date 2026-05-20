// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

func TestValidDigest(t *testing.T) {
	t.Parallel()

	valid := "sha256:" + strings.Repeat("a", 64)
	for _, ok := range []string{valid, "sha256:" + strings.Repeat("0", 64)} {
		if !container.ValidDigest(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}

	for _, bad := range []string{
		"",
		strings.Repeat("a", 64),             // missing sha256: prefix
		"sha256:" + strings.Repeat("a", 63), // too short
		"sha256:" + strings.Repeat("A", 64), // uppercase not allowed
		"sha512:" + strings.Repeat("a", 64), // wrong algo
	} {
		if container.ValidDigest(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestStripTag(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ghcr.io/org/app:v1.2.3": "ghcr.io/org/app",    // registry + tag
		"ghcr.io/org/app":        "ghcr.io/org/app",    // already bare (idempotent)
		"localhost:5000/img:tag": "localhost:5000/img", // host:port preserved
		"localhost:5000/img":     "localhost:5000/img", // host:port, no tag
		"alpine:3.21":            "alpine",             // registry-less
	}
	for in, want := range cases {
		if got := container.StripTag(in); got != want {
			t.Errorf("StripTag(%q) = %q, want %q", in, got, want)
		}
	}
}
