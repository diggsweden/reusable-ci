// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestRegistryAuth_MatchesRegistry(t *testing.T) {
	t.Parallel()

	auth := provider.RegistryAuth{Registry: "ghcr.io"}

	tests := []struct {
		target string
		want   bool
	}{
		{"", true},                     // caller defaulted to the forge registry
		{"ghcr.io", true},              // exact
		{"GHCR.IO", true},              // case-insensitive
		{"ghcr.io/owner/image", true},  // host extracted from a full ref
		{"https://ghcr.io", true},      // scheme stripped
		{"docker.io", false},           // unrelated registry — must not match
		{"registry.gitlab.com", false}, // unrelated registry
	}

	for _, tc := range tests {
		if got := auth.MatchesRegistry(tc.target); got != tc.want {
			t.Errorf("MatchesRegistry(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}
