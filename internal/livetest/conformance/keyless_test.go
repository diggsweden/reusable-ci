// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-SIGN-2: keyless signing against a Sigstore the lab owns.
//
// The lab runs its own Fulcio and trusts the issuers its contract names. A job
// asks its forge for an ID token, the product hands it to that CA, and the
// certificate that comes back names the pipeline. That covers the
// MintsOIDCToken capability: the forge issues tokens a Fulcio accepts, and the
// product can be pointed at one. It says nothing about public Sigstore's
// policies, which is the separate PublicFulcioTrusted claim.
//
// Both live forges run it. Adding a third needs two things: an issuer entry in
// the CA (which the contract then advertises through LAB_FULCIO_ISSUERS) and a
// way for its runner to hand the job a token.
//
// No transparency log takes part: the suite runs with
// REUSABLE_CI_COSIGN_TRANSPARENCY=none and the lab deploys no Rekor, so a lab
// signature cannot reach the public log.

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// claimsMintsOIDCToken selects forges whose capability matrix says they can
// mint an OIDC token for a Fulcio that trusts them. That is the claim this
// scenario tests: the lab runs its own CA, so "public Fulcio trusts the issuer"
// (PublicFulcioTrusted) is the wrong question here and would exclude Forgejo, which can
// sign perfectly well against a CA told to accept it.
func claimsMintsOIDCToken(c provider.Capabilities) bool { return c.MintsOIDCToken }

func TestInRunner_KeylessSigningAgainstTheLabCA(t *testing.T) {
	const tag = "v0.0.10-keyless"

	for _, kind := range forgesClaiming(t, claimsMintsOIDCToken, "keyless signing against an own CA") {
		if !livetest.Requires(t, kind, livetest.NeedsInRunner, livetest.NeedsFulcio) {
			continue
		}

		// The URL itself, now that the environment is known to have one.
		fulcioURL, _ := livetest.FulcioURL()

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			// A fresh path per run: GitLab will not issue an ID token for a
			// repository path that has been used and deleted before.
			repo := livetest.NewScratchRepoUnique(t, target, "keyless")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "keyless-sign",
				keylessSignProbe(kind, assetURL, fulcioURL, keylessIssuer(kind, target.BaseURL())))
			if conclusion != "success" {
				t.Errorf("%s: keyless signing concluded %q — this forge mints OIDC tokens, but one did not produce a signing certificate from the lab CA",
					kind, conclusion)
			}
		})
	}
}

// keylessIssuer is the issuer a job's token will claim on this forge.
//
// GitLab's is the instance URL; Forgejo appends /api/actions, which is what its
// own discovery document advertises. Fulcio matches the issuer by EXACT string,
// so the path is not cosmetic — a host-only URL is refused with "There was an
// error processing the identity token", which names neither the path nor the
// port.
//
// Spelled here rather than taken from the product because the product
// deliberately auto-supplies neither: no public Fulcio trusts either instance,
// so a keyless run passes --oidc-issuer, and this is the value it passes.
func keylessIssuer(kind provider.Platform, baseURL string) string {
	if kind == provider.PlatformForgejo {
		return strings.TrimRight(baseURL, "/") + "/api/actions"
	}

	return baseURL
}

// keylessSignProbe signs a file with a runner-minted token against the lab CA.
// The assertions run inside the job; nothing is shipped back out.
func keylessSignProbe(kind provider.Platform, assetURL, fulcioURL, issuerURL string) string {
	// Obtaining the token is the only step that differs. GitLab's runner mints it
	// into $SIGSTORE_ID_TOKEN through the id_tokens: block; Forgejo speaks the
	// GitHub Actions protocol, handing the job a token ENDPOINT instead, so the
	// job fetches from it and exports the same variable. Everything downstream is
	// then identical, and cosign reads that variable by name on both.
	tokenSetup := ""
	identity := `^https://.*/${CI_PROJECT_PATH}//?\.gitlab-ci\.yml@`

	if kind == provider.PlatformForgejo {
		//nolint:gosec // G101 false positive: shell that READS a token endpoint from the job's environment. No credential is embedded here — the value exists only inside the runner.
		tokenSetup = `if [ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ]; then
  echo "FAIL: enable-openid-connect injected no token endpoint"
  exit 1
fi
# The audience must be sigstore, which is what the lab's Fulcio expects for this
# issuer; Forgejo defaults it to <instance>/<owner> unless asked.
SIGSTORE_ID_TOKEN="$(curl -fsSL \
  -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
  "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=sigstore" | jq -r '.value')"
export SIGSTORE_ID_TOKEN
`
		// Forgejo's SAN is <server>/<workflow_ref>, and workflow_ref is
		// <owner>/<repo>/.forgejo/workflows/<file>@<git ref>.
		identity = `^https://.*/${GITHUB_REPOSITORY}/\.forgejo/workflows/.*@`
	}

	body := `apk add --no-cache curl jq >/dev/null

# cosign 3.x, pinned rather than the distribution package: Alpine ships
# 2.4.3, and the adapter's bundle handling and signing-config documents
# target 3.x.
curl -fsSL -o /usr/local/bin/cosign \
  https://github.com/sigstore/cosign/releases/download/v3.1.2/cosign-linux-amd64
chmod +x /usr/local/bin/cosign
cosign version 2>&1 | grep -i gitversion

` + livetest.ProbePrelude(assetURL) + `

` + tokenSetup + `
if [ -z "${SIGSTORE_ID_TOKEN:-}" ]; then
  echo "FAIL: the runner minted no id_token, so there is no identity to sign with"
  exit 1
fi

# The trust anchor is built from what the CA serves: cosign has no way to
# learn a private CA's root, and fetching it survives a CA rotation.
curl -fsS "` + fulcioURL + `/api/v2/trustBundle" \
  | jq -r '.chains[0].certificates[]' > fulcio-root.pem
test -s fulcio-root.pem

cosign trusted-root create \
  --fulcio="url=` + fulcioURL + `,certificate-chain=fulcio-root.pem" \
  --no-default-fulcio --no-default-rekor --no-default-ctfe --no-default-tsa \
  --out trusted-root.json

# .tgz, not .txt: the sign verb filters by extension
# (.jar .tgz .tar.gz .zip .war).
mkdir -p dist
echo "par-sign-2 payload" > dist/artifact.tgz

run_product release sign \
  --method=sigstore \
  --release-artifacts-dir dist \
  --no-checksums-file \
  --oidc-issuer "` + issuerURL + `" \
  --fulcio-url "` + fulcioURL + `" \
  --trusted-root trusted-root.json

# Listed before it is searched: testing a guessed filename fails silently
# under set -e, so a renamed sidecar would read as "signing broke". The
# cwd is searched alongside dist because the sign verb moves each sidecar
# next to the cwd where the upload step collects them.
echo "--- dist ---"
ls -la dist .

bundle="$(find . -maxdepth 2 -name '*.bundle' -type f | head -n 1)"
if [ -z "$bundle" ]; then
  echo "FAIL: signing reported success but produced no bundle"
  exit 1
fi
echo "--- bundle: $bundle ---"
head -c 300 "$bundle"; echo

# Authority: the certificate must come from the lab CA, not public
# Sigstore. Identity: it must name this pipeline, so the subject is pinned
# to this job's workflow -- what Fulcio puts in the SAN -- rather than a .*
# that would accept any identity from any issuer.
cosign verify-blob \
  --bundle "$bundle" \
  --certificate-oidc-issuer "` + issuerURL + `" \
  --certificate-identity-regexp "` + identity + `" \
  --trusted-root trusted-root.json \
  --insecure-ignore-tlog \
  --insecure-ignore-sct \
  dist/artifact.tgz

echo "keyless signature verified against the lab CA"`

	if kind == provider.PlatformGitLab {
		return `sign:
  image: docker.io/library/alpine:3.22
  id_tokens:
    # cosign reads this variable by name. The lab's Fulcio expects the
    # sigstore audience for this issuer.
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  script:
    - |
      ` + indent(body, 6) + `
`
	}

	// enable-openid-connect is what injects the token endpoint. At workflow
	// level so the job inherits it; Forgejo disables it for pull_request events
	// from forks, and this scenario triggers on push.
	return `on: [push]
enable-openid-connect: true
jobs:
  sign:
    runs-on: ubuntu-latest
    container:
      image: docker.io/library/alpine:3.22
    steps:
      - name: keyless-sign against the lab CA
        run: |
          ` + indent(body, 10) + `
`
}
