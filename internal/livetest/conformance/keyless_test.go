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
// The probe below is GitLab-shaped (`id_tokens:`, $CI_SERVER_TLS_CA_FILE). A
// second forge needs both an issuer entry in the CA and an Actions-flavoured
// probe before it can run here.
//
// No transparency log takes part: the suite runs with
// REUSABLE_CI_COSIGN_TRANSPARENCY=none and the lab deploys no Rekor, so a lab
// signature cannot reach the public log.

import (
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
		if !livetest.RunsInRunner(kind) {
			t.Logf("SKIP %s: claims keyless OIDC, but the in-runner tier does not drive this forge yet", kind)

			continue
		}

		fulcioURL, hasCA := livetest.FulcioURL()
		if !hasCA {
			t.Logf("SKIP %s: this environment provides no Fulcio (LAB_FULCIO_URL unset)", kind)

			continue
		}

		if !livetest.FulcioTrusts(kind) {
			// Which issuers the CA accepts is the environment's business, read
			// from the contract rather than assumed here: a forge needs its own
			// entry in the CA's issuer config before this scenario can mean
			// anything for it.
			t.Logf("SKIP %s: this environment's Fulcio is not configured to trust it (LAB_FULCIO_ISSUERS)", kind)

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
				t.Errorf("%s: keyless signing concluded %q — this forge reports PublicFulcioTrusted, but a job's token did not produce a signing certificate",
					kind, conclusion)
			}
		})
	}
}

// keylessSignProbe signs a file with a GitLab-minted token against the lab CA.
// The assertions run inside the job; nothing is shipped back out.
func keylessSignProbe(assetURL, fulcioURL, issuerURL string) string {
	return `sign:
  image: docker.io/library/alpine:3.22
  id_tokens:
    # cosign reads this variable by name. The lab's Fulcio expects the
    # sigstore audience for this issuer.
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  script:
    - |
      apk add --no-cache curl jq >/dev/null

      # cosign 3.x, pinned rather than the distribution package: Alpine ships
      # 2.4.3, and the adapter's bundle handling and signing-config documents
      # target 3.x.
      curl -fsSL -o /usr/local/bin/cosign \
        https://github.com/sigstore/cosign/releases/download/v3.1.2/cosign-linux-amd64
      chmod +x /usr/local/bin/cosign
      cosign version 2>&1 | grep -i gitversion

      ` + indent(livetest.ProbePrelude(assetURL), 6) + `

      # Trust the lab CA: every service here speaks TLS signed by it and cosign
      # has no --insecure equivalent. GitLab hands the runner's CA to the job in
      # this variable.
      if [ ! -s "${CI_SERVER_TLS_CA_FILE:-}" ]; then
        echo "FAIL: no CI_SERVER_TLS_CA_FILE; the job cannot trust the lab CA"
        exit 1
      fi
      # Appended to the system roots, not substituted for them: SSL_CERT_FILE
      # pointed at the lab CA alone breaks every public TLS client in the job.
      cat "$CI_SERVER_TLS_CA_FILE" >> /etc/ssl/certs/ca-certificates.crt

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
      # to this project's CI configuration -- what Fulcio puts in the SAN --
      # rather than a .* that would accept any identity from any issuer.
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
