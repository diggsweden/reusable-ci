// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// TestProvenanceFromEnv_ResolvesForgejoNativeNames pins that the container
// provenance reads the run context through the shared chains: a Forgejo
// runner that sets only its native FORGEJO_* names (no GITHUB_* aliases)
// still yields a source, ref and commit dependency.
func TestProvenanceFromEnv_ResolvesForgejoNativeNames(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("FORGEJO_ACTIONS", "true")
	env.Setenv("FORGEJO_SERVER_URL", "https://forge.example/")
	env.Setenv("FORGEJO_REPOSITORY", "owner/app")
	env.Setenv("FORGEJO_REF_NAME", "v1.2.3")
	env.Setenv("FORGEJO_SHA", "0123456789abcdef0123456789abcdef01234567")

	got := provenanceFromEnv("forge.example/owner/app@sha256:abc")

	if got.SourceURI != "git+https://forge.example/owner/app" {
		t.Errorf("SourceURI = %q, want the Forgejo-native repository", got.SourceURI)
	}

	if got.Ref != "v1.2.3" {
		t.Errorf("Ref = %q, want v1.2.3", got.Ref)
	}

	if len(got.ResolvedDeps) != 1 {
		t.Fatalf("ResolvedDeps = %+v, want the one source dependency", got.ResolvedDeps)
	}
}
