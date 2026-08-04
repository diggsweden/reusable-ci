// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-SIGN-2: keyless signing, against a Sigstore this lab owns.
//
// The last capability the suite reported without evidence. `KeylessOIDC: true`
// meant "this forge publishes an OIDC issuer", derived from the adapter, and
// nothing ever exchanged a token for a certificate to find out whether that was
// usable. It could not: the only certificate authority available was the public
// Sigstore, and signing against it publishes to a transparency log that cannot
// be unpublished. A lab must never do that.
//
// So the lab runs its own Fulcio, trusting exactly one issuer — this lab's
// GitLab — and this scenario drives the whole path: a job asks GitLab for an ID
// token, the product hands it to a CA that will actually accept it, and a
// certificate comes back naming the pipeline that asked.
//
// What that proves, and what it does not. It proves the claim the capability
// makes: this forge issues OIDC tokens a Fulcio accepts, and the product can be
// pointed at one. It does not prove anything about public Sigstore's policies,
// which are its own business and out of reach from here by design.
//
// No transparency log takes part. The suite runs with
// REUSABLE_CI_COSIGN_TRANSPARENCY=none, and the lab deploys no Rekor: publishing
// is a separate claim, and the containment rule is absolute — a lab signature
// must never reach the public log.

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// claimsKeylessOIDC selects the forges whose capability matrix says they can do
// this. A forge that claims it and cannot is the failure being hunted.
func claimsKeylessOIDC(c provider.Capabilities) bool { return c.KeylessOIDC }

func TestInRunner_KeylessSigningAgainstTheLabCA(t *testing.T) {
	const tag = "v0.0.10-keyless"

	for _, kind := range forgesClaiming(t, claimsKeylessOIDC, "keyless OIDC signing") {
		if !livetest.RunsInRunner(kind) {
			t.Logf("SKIP %s: claims keyless OIDC, but the in-runner tier does not drive this forge yet", kind)

			continue
		}

		fulcioURL, hasCA := livetest.FulcioURL()
		if !hasCA {
			t.Logf("SKIP %s: this environment provides no Fulcio (LAB_FULCIO_URL unset)", kind)

			continue
		}

		if kind != provider.PlatformGitLab {
			// The lab's Fulcio trusts one issuer. A second forge needs its own
			// entry there before this can mean anything for it, and saying so
			// beats a scenario that silently covers one forge.
			t.Logf("SKIP %s: the lab CA is configured to trust GitLab only", kind)

			continue
		}

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			// A fresh path per run: GitLab will not issue an ID token for a
			// repository path that has been used and deleted before.
			repo := livetest.NewScratchRepoUnique(t, target, "keyless")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "keyless-sign",
				keylessSignProbe(assetURL, fulcioURL, target.BaseURL()))
			if conclusion != "success" {
				t.Errorf("%s: keyless signing concluded %q — this forge reports KeylessOIDC, but a job's token did not produce a signing certificate",
					kind, conclusion)
			}
		})
	}
}

// keylessSignProbe signs a file with a token this lab's GitLab minted, against
// a CA this lab runs.
//
// The assertions are inside the job because that is where the evidence is: the
// bundle carries the certificate, and the certificate carries the identity of
// the pipeline that asked for it. Checking that here would mean shipping the
// bundle out of the job to prove something the job can prove itself.
func keylessSignProbe(assetURL, fulcioURL, issuerURL string) string {
	return `sign:
  image: docker.io/library/alpine:3.22
  id_tokens:
    # cosign reads this variable by name; the audience must be sigstore, which
    # is what the lab's Fulcio is configured to expect for this issuer.
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  script:
    - |
      apk add --no-cache curl jq >/dev/null

      # cosign 3.x, pinned, NOT the distribution package. Alpine ships 2.4.3,
      # a major version behind what this product is written against -- the
      # adapter's bundle handling and signing-config documents target 3.x, so
      # testing against 2.x would exercise different semantics and call the
      # result parity.
      curl -fsSL -o /usr/local/bin/cosign \
        https://github.com/sigstore/cosign/releases/download/v3.1.2/cosign-linux-amd64
      chmod +x /usr/local/bin/cosign
      cosign version 2>&1 | grep -i gitversion

      ` + indent(livetest.ProbePrelude(assetURL), 6) + `

      # Trust the lab CA. Every service here speaks TLS signed by it, and cosign
      # offers no --insecure equivalent -- correctly, since a signing tool that
      # skips certificate checks is not signing anything meaningful. GitLab
      # hands the runner's CA to the job in this variable.
      if [ ! -s "${CI_SERVER_TLS_CA_FILE:-}" ]; then
        echo "FAIL: no CI_SERVER_TLS_CA_FILE; the job cannot trust the lab CA"
        exit 1
      fi
      # APPENDED to the system roots, not substituted for them. Pointing
      # SSL_CERT_FILE at the lab CA alone makes every public TLS client in the
      # job fail -- apk cannot reach its repositories, and the failure names a
      # certificate rather than the substitution that caused it.
      cat "$CI_SERVER_TLS_CA_FILE" >> /etc/ssl/certs/ca-certificates.crt

      # The token must exist before anything else is worth trying.
      if [ -z "${SIGSTORE_ID_TOKEN:-}" ]; then
        echo "FAIL: the runner minted no id_token, so there is no identity to sign with"
        exit 1
      fi

      # A release-artifact extension: the sign verb filters by extension
      # (.jar .tgz .tar.gz .zip .war), so a .txt is skipped and nothing signs.
      # cosign verifies the certificate it was just issued, and has no way to
      # learn a private CA's root -- so the trust anchor is built from what the
      # CA itself serves, rather than pinned. Reading it from the running
      # instance is also what makes an ephemeral or rotated CA a non-event here.
      curl -fsS "` + fulcioURL + `/api/v2/trustBundle" \
        | jq -r '.chains[0].certificates[]' > fulcio-root.pem
      test -s fulcio-root.pem

      cosign trusted-root create \
        --fulcio="url=` + fulcioURL + `,certificate-chain=fulcio-root.pem" \
        --no-default-fulcio --no-default-rekor --no-default-ctfe --no-default-tsa \
        --out trusted-root.json

      mkdir -p dist
      echo "par-sign-2 payload" > dist/artifact.tgz

      run_product release sign \
        --method=sigstore \
        --release-artifacts-dir dist \
        --no-checksums-file \
        --oidc-issuer "` + issuerURL + `" \
        --fulcio-url "` + fulcioURL + `" \
        --trusted-root trusted-root.json

      # A bundle is not proof on its own: the interesting question is which
      # authority vouched for it, and for whom.
      #
      # The directory is listed before anything is asserted about it. Testing a
      # guessed filename fails silently under set -e, which is how a renamed
      # sidecar reads as "signing broke" instead of "look one line up".
      # Looked for in the working directory as well as dist: the sign verb moves
      # each sidecar next to the cwd rather than leaving it beside the artifact,
      # because that is where the upload step collects them from.
      echo "--- dist ---"
      ls -la dist .

      bundle="$(find . -maxdepth 2 -name '*.bundle' -type f | head -n 1)"
      if [ -z "$bundle" ]; then
        echo "FAIL: signing reported success but produced no bundle"
        exit 1
      fi
      echo "--- bundle: $bundle ---"
      head -c 300 "$bundle"; echo

      # Two claims, and the identity one is the point of keyless signing.
      #
      # The authority: the certificate must come from the lab CA, not public
      # Sigstore. If the CA were ignored the run would either have failed
      # reaching sigstore.dev or succeeded against the wrong authority.
      #
      # The identity: the certificate must name THIS pipeline. A regexp of .*
      # would accept a certificate for any identity from any issuer, which is
      # the assertion that makes a keyless test look green while proving
      # nothing -- so the subject is pinned to this project's own CI
      # configuration, which is what Fulcio put in the SAN.
      cosign verify-blob \
        --bundle "$bundle" \
        --certificate-oidc-issuer "` + issuerURL + `" \
        --certificate-identity-regexp "^https://.*/${CI_PROJECT_PATH}//?\.gitlab-ci\.yml@" \
        --trusted-root trusted-root.json \
        --insecure-ignore-tlog \
        --insecure-ignore-sct \
        dist/artifact.tgz

      echo "keyless signature verified against the lab CA"
`
}
