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

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
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

	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "an OCI registry"), livetest.NeedsInRunner) {

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "regauth")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "registry-auth",
				registryAuthProbe(kind, assetURL))
			if conclusion != "success" {
				t.Errorf("%s: run concluded %q — the forge-injected registry credential was not found or does not authenticate, so a pipeline relying on the fallback must carry a standing secret instead",
					kind, conclusion)
			}
		})
	}
}

// registryAuthVerify is the shared half: given a registry host and the base64
// credential the product stored, prove the registry refuses anonymous access and
// accepts that credential. Written once because the difference between the
// forges is a property of registries, not of forges.
const registryAuthVerify = `
# Refuse to prove anything against an anonymously-readable registry.
anon="$(curl -s -o /dev/null -w '%{http_code}' "https://$registry/v2/")"
echo "anonymous /v2/ -> $anon"
if [ "$anon" = 200 ]; then
  echo "FAIL: this registry serves /v2/ without credentials, so an authenticated check proves nothing"
  exit 1
fi

# Flow 1: HTTP Basic, which the Gitea family accepts directly.
code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Basic $stored" "https://$registry/v2/")"
echo "basic /v2/ -> $code"

if [ "$code" != 200 ]; then
  # Flow 2: the Bearer challenge — exchange the credential at the realm the
  # registry names, then retry. This is what a docker client does.
  challenge="$(curl -s -D - -o /dev/null "https://$registry/v2/" | tr -d '\r' | grep -i '^www-authenticate:' || true)"
  echo "challenge: $challenge"

  realm="$(printf '%s' "$challenge" | sed -n 's/.*realm="\([^"]*\)".*/\1/p')"
  service="$(printf '%s' "$challenge" | sed -n 's/.*service="\([^"]*\)".*/\1/p')"
  if [ -z "$realm" ]; then
    echo "FAIL: registry refused basic auth and offered no bearer realm to exchange at"
    exit 1
  fi

  token="$(curl -s -H "Authorization: Basic $stored" "$realm?service=$service" \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
  if [ -z "$token" ]; then
    echo "FAIL: the stored credential was rejected by the token service at $realm"
    exit 1
  fi

  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "https://$registry/v2/")"
  echo "bearer /v2/ -> $code"
fi

if [ "$code" != 200 ]; then
  echo "FAIL: the credential the product resolved does not authenticate to $registry"
  exit 1
fi

echo "the forge-injected credential authenticates to $registry"
`

// registryAuthProbe logs in with no credentials of its own and then proves what
// was stored actually works. The registry is taken from the runner's own
// environment on both forges, so the scenario cannot pass by agreeing with a
// value the fixture invented.
func registryAuthProbe(kind provider.Platform, assetURL string) string {
	const extractStored = `
stored="$(sed -n 's/.*"auth"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' auth.json | head -n 1)"
if [ -z "$stored" ]; then
  echo "FAIL: container login wrote no credential for $registry"
  cat auth.json
  exit 1
fi`

	if kind == provider.PlatformGitLab {
		return `detect:
  image: quay.io/podman/stable:v5.6.2
  script:
    - |
      ` + indent(livetest.ProbePrelude(assetURL), 6) + `

      # GitLab names its registry in the job environment; using anything else
      # would test the fixture rather than the forge.
      registry="$CI_REGISTRY"
      echo "CI_REGISTRY=$registry"
      test -n "$registry"

      # No --registry-username and no password: the fallback is what is under test.
      run_product container login --registry "$registry" --auth-file auth.json
      ` + indent(extractStored, 6) + `
      ` + indent(registryAuthVerify, 6) + `
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the forge-injected registry credential must be found and work
        run: |
          ` + indent(livetest.ProbePrelude(assetURL), 10) + `

          # Forgejo serves its container registry from the forge host itself.
          server="${FORGEJO_SERVER_URL:-$GITHUB_SERVER_URL}"
          registry="${server#https://}"
          registry="${registry#http://}"
          echo "server=$server registry=$registry"
          test -n "$registry"

          run_product container login --registry "$registry" --auth-file auth.json
          ` + indent(extractStored, 10) + `
          ` + indent(registryAuthVerify, 10) + `
`
}
