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
	for _, forge := range forgesClaiming(t, alwaysValidatesTokens, "an OCI registry") {
		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
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
						t.Fatalf("%s: push reported no digest", forge)
					}

					served, found := livetest.ImageDigest(t, target, repo, tag)
					if !found {
						t.Fatalf("%s: registry serves nothing at %s", forge, pushed.Ref)
					}

					if served != pushed.Digest {
						t.Errorf("%s: registry serves %s, push reported %s", forge, served, pushed.Digest)
					}
				})
			}

			// A tag that was never pushed must read as absent rather than as an
			// error: ledger verification depends on telling those apart.
			if _, found := livetest.ImageDigest(t, target, repo, "no-such-tag"); found {
				t.Errorf("%s: reports a digest for a tag that was never pushed", forge)
			}
		})
	}
}

// PAR-REG-5: the forge's own runner-injected registry credential is found, and
// it works.
//
// `container login` falls back to the credential the forge injects into a job
// when no --registry-password is given. That fallback is the whole point of the
// RegistryAuthResolver role — it is what lets a pipeline push an image without
// a standing secret — and it could not be tested anywhere before this tier,
// because the credential does not exist outside a run. GitLab is explicit about
// it: $CI_REGISTRY_PASSWORD is the job token and is "valid only as long as the
// job is running"
// (https://docs.gitlab.com/ci/variables/predefined_variables/).
//
// Resolution itself is pure environment reading, which unit tests already cover.
// What only a real job can settle is whether the thing resolved is *usable*, so
// the probe finishes by authenticating to the registry with the credential the
// product stored, rather than stopping at "a file was written".
//
// The forges answer that handshake differently and a client has to handle both,
// so the probe does too rather than branching per forge: Forgejo's registry
// accepts HTTP Basic on /v2/ directly, while GitLab answers 401 with a Bearer
// challenge naming a token service, which must then be exchanged. Both are the
// OCI distribution spec's auth flow
// (https://distribution.github.io/distribution/spec/auth/token/).
//
// Self-validating: the probe first requires the registry to REFUSE an
// unauthenticated request. Against an anonymously-readable registry every
// authenticated check below would pass while proving nothing at all.
func TestInRunner_ForgeInjectedRegistryCredentialAuthenticates(t *testing.T) {
	const tag = "v0.0.7-regauth"

	for _, forge := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "an OCI registry"), livetest.NeedsInRunner) {

		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			repo := livetest.NewScratchRepo(t, target, "regauth")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "registry-auth",
				livetest.RegistryAuthProbe(target, assetURL))
			if conclusion != "success" {
				t.Errorf("%s: run concluded %q — the forge-injected registry credential was not found or does not authenticate, so a pipeline relying on the fallback must carry a standing secret instead",
					forge, conclusion)
			}
		})
	}
}
