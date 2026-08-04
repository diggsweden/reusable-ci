// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-REG-1: the registry fixture itself, on every forge.
//
// Before any ledger scenario can mean anything, an image has to reach the
// forge's registry and be served back under the digest it was pushed with. That
// is not a given across forges: GitLab runs a separate registry host while the
// Gitea family serves packages from the forge host, and the auth handshake
// differs behind both. This proves the fixture before the ledger depends on it.

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestRegistry_SyntheticArtifacts_RoundTripByDigest(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "an OCI registry") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "registry")

			for _, shape := range []struct {
				name string
				push func(livetest.TB, livetest.Target, string, string) livetest.Image
			}{
				// Both shapes, because promotion copies a tag and an index is
				// where a copy that does not preserve the digest goes wrong.
				{name: "image", push: livetest.PushImage},
				{name: "index", push: livetest.PushIndex},
			} {
				t.Run(shape.name, func(t *testing.T) {
					tag := "staging-v0.0.1-" + shape.name

					pushed := shape.push(t, target, repo, tag)
					if pushed.Digest == "" {
						t.Fatalf("%s: push reported no digest", kind)
					}

					served, found := livetest.ImageDigest(t, target, repo, tag)
					if !found {
						t.Fatalf("%s: registry serves nothing at %s", kind, pushed.Ref)
					}

					if served != pushed.Digest {
						t.Errorf("%s: registry serves %s, push reported %s", kind, served, pushed.Digest)
					}
				})
			}

			// A tag that was never pushed must read as absent rather than as an
			// error: ledger verification depends on telling those apart.
			if _, found := livetest.ImageDigest(t, target, repo, "no-such-tag"); found {
				t.Errorf("%s: reports a digest for a tag that was never pushed", kind)
			}
		})
	}
}
