// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

const registryAuthVerifyTemplate = `
registry_url="https://$registry"
__REGISTRY_AUTHORITY_GUARD__

# Refuse to prove anything against an anonymously-readable registry.
anon="$(curl -s -o /dev/null -w '%{http_code}' "$registry_url/v2/")"
echo "anonymous /v2/ -> $anon"
if [ "$anon" = 200 ]; then
  echo "FAIL: this registry serves /v2/ without credentials, so an authenticated check proves nothing"
  exit 1
fi

# Flow 1: HTTP Basic, which the Gitea family accepts directly.
code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Basic $stored" "$registry_url/v2/")"
echo "basic /v2/ -> $code"

if [ "$code" != 200 ]; then
  challenge="$(curl -s -D - -o /dev/null "$registry_url/v2/" | tr -d '\r' | grep -i '^www-authenticate:' || true)"
  echo "challenge: $challenge"

  realm="$(printf '%s' "$challenge" | sed -n 's/.*realm="\([^"]*\)".*/\1/p')"
  service="$(printf '%s' "$challenge" | sed -n 's/.*service="\([^"]*\)".*/\1/p')"
  if [ -z "$realm" ]; then
    echo "FAIL: registry refused basic auth and offered no bearer realm to exchange at"
    exit 1
  fi

__CREDENTIAL_AUTHORITY_GUARD__

  token="$(curl -s -H "Authorization: Basic $stored" "$realm?service=$service" \
    | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
  if [ -z "$token" ]; then
    echo "FAIL: the stored credential was rejected by the validated token service"
    exit 1
  fi

  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "$registry_url/v2/")"
  echo "bearer /v2/ -> $code"
fi

if [ "$code" != 200 ]; then
  echo "FAIL: the resolved credential did not authenticate to the validated registry"
  exit 1
fi

unset stored token
rm -f auth.json
if [ "$credential_trace" = 1 ]; then
  set -x
fi
echo "the forge-injected credential authenticates to the validated registry"
`

// RegistryAuthProbe returns the in-runner registry credential probe without
// embedding or printing any credential value or registry auth file.
func RegistryAuthProbe(target Target, assetURL string) string {
	const extractStored = `
credential_trace=0
case $- in
  *x*) credential_trace=1; set +x ;;
esac
stored="$(sed -n 's/.*"auth"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' auth.json | head -n 1)"
if [ -z "$stored" ]; then
  echo "FAIL: container login wrote no usable credential"
  exit 1
fi`

	verify := registryAuthVerify(target)
	if target.Forge == provider.ForgeGitLab {
		return `detect:
  image: ` + ProbeImage + `
  script:
    - |
      ` + indentProbe(ProbePrelude(target, assetURL), 6) + `

      registry="$CI_REGISTRY"
      echo "CI_REGISTRY=$registry"
      test -n "$registry"

      run_product container login --registry "$registry" --auth-file auth.json
` + indentProbe(extractStored, 6) + "\n" + indentProbe(verify, 6) + "\n"
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the forge-injected registry credential must be found and work
        run: |
          ` + indentProbe(ProbePrelude(target, assetURL), 10) + `

          server="${FORGEJO_SERVER_URL:-$GITHUB_SERVER_URL}"
          registry="${server#https://}"
          registry="${registry#http://}"
          echo "server=$server registry=$registry"
          test -n "$registry"

          run_product container login --registry "$registry" --auth-file auth.json
` + indentProbe(extractStored, 10) + "\n" + indentProbe(verify, 10) + "\n"
}

func registryAuthVerify(target Target) string {
	registryGuard := credentialAuthorityGuard([]string{target.RegistryOrigin}, "registry_url", false)
	realmGuard := credentialAuthorityGuard(target.RegistryAuthorities, "realm", true)
	withRegistryGuard := strings.Replace(registryAuthVerifyTemplate, "__REGISTRY_AUTHORITY_GUARD__", registryGuard, 1)

	return strings.Replace(withRegistryGuard, "__CREDENTIAL_AUTHORITY_GUARD__", realmGuard, 1)
}

func credentialAuthorityGuard(authorities []string, variable string, allowPaths bool) string {
	var guard strings.Builder
	guard.WriteString("  case \"$" + variable + "\" in\n")

	for _, authority := range authorities {
		guard.WriteString("    " + strconv.Quote(authority))

		if allowPaths {
			guard.WriteString("|" + strconv.Quote(authority+"/") + "*")
		}

		guard.WriteString(") ;;\n")
	}

	guard.WriteString(`    *)
      echo "FAIL: credential authority is outside the validated contract allowlist"
      exit 1
      ;;
  esac`)

	return guard.String()
}

// KeylessSignProbe returns a keyless signing workflow whose token-bearing
// network phase is constrained to one validated Fulcio CONNECT authority.
func KeylessSignProbe(target Target, assetURL, proxyAssetURL, fulcioURL, oidcIssuer string) string {
	if len(target.OIDCAuthorities) != 1 {
		panic("livetest: keyless probe requires one validated Fulcio authority")
	}

	body := keylessSignScript(target, assetURL, proxyAssetURL, fulcioURL, oidcIssuer)

	if target.Forge == provider.ForgeGitLab {
		return `sign:
  image: docker.io/library/alpine:3.22
  id_tokens:
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  script:
    - |
      ` + indentProbe(body, 6) + `
`
	}

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
          ` + indentProbe(body, 10) + `
`
}

func keylessSignScript(target Target, assetURL, proxyAssetURL, fulcioURL, oidcIssuer string) string {
	//nolint:gosec // The generated shell names runner variables but embeds no credential value.
	credentialSetup := `case $- in *x*) set +x; credential_trace=1 ;; *) credential_trace=0 ;; esac; unset rc_sigstore_id_token; rc_sigstore_id_token="${SIGSTORE_ID_TOKEN:-}"; unset SIGSTORE_ID_TOKEN
if [ -z "$rc_sigstore_id_token" ]; then
  echo "FAIL: the runner minted no id_token, so there is no identity to sign with"
  exit 1
fi
`
	tokenFetch := ""
	identity := `^https://.*/${CI_PROJECT_PATH}//?\.gitlab-ci\.yml@`
	fulcioAuthority := target.OIDCAuthorities[0]

	if target.Forge == provider.ForgeForgejo {
		//nolint:gosec // The generated shell names runner variables but embeds no credential value.
		credentialSetup = `case $- in *x*) set +x; credential_trace=1 ;; *) credential_trace=0 ;; esac; unset rc_actions_id_token_request_url rc_actions_id_token_request_token rc_sigstore_id_token; rc_actions_id_token_request_url="${ACTIONS_ID_TOKEN_REQUEST_URL:-}"; rc_actions_id_token_request_token="${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}"; unset ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN
if [ -z "$rc_actions_id_token_request_url" ] || [ -z "$rc_actions_id_token_request_token" ]; then
  echo "FAIL: enable-openid-connect injected no token endpoint"
  exit 1
fi
`
		tokenFetch = credentialAuthorityGuard(target.ForgeAuthorities, "rc_actions_id_token_request_url", true) + `
if ! rc_token_response="$(curl -fsS --proto '=https' --location --max-redirs 0 \
  -H "Authorization: bearer $rc_actions_id_token_request_token" \
  "${rc_actions_id_token_request_url}&audience=sigstore")"; then
  echo "FAIL: the validated forge token endpoint did not mint an id_token"
  exit 1
fi
if ! rc_sigstore_id_token="$(printf '%s' "$rc_token_response" | jq -er '.value | select(type == "string" and length > 0)')"; then
  echo "FAIL: the validated forge token endpoint returned no id_token"
  exit 1
fi
unset rc_token_response rc_actions_id_token_request_url rc_actions_id_token_request_token
`
		identity = `^https://.*/${GITHUB_REPOSITORY}/\.forgejo/workflows/.*@`
	}

	return credentialSetup + `apk add --no-cache curl jq >/dev/null

curl -fsSL -o /usr/local/bin/cosign \
  https://github.com/sigstore/cosign/releases/download/v3.1.2/cosign-linux-amd64
chmod +x /usr/local/bin/cosign
cosign version 2>&1 | grep -i gitversion

` + ProbePrelude(target, assetURL) + `
curl -fsSL -o credential-proxy "` + proxyAssetURL + `"
chmod +x credential-proxy

` + tokenFetch + `if [ -z "$rc_sigstore_id_token" ]; then
  echo "FAIL: the runner minted no id_token, so there is no identity to sign with"
  exit 1
fi

proxy_ready="$PWD/credential-proxy.ready"
env -u SIGSTORE_ID_TOKEN \
  -u ACTIONS_ID_TOKEN_REQUEST_URL \
  -u ACTIONS_ID_TOKEN_REQUEST_TOKEN \
  -u rc_sigstore_id_token \
  -u rc_actions_id_token_request_url \
  -u rc_actions_id_token_request_token \
  ./credential-proxy --authority "` + fulcioAuthority + `" --ready-file "$proxy_ready" &
fulcio_proxy_pid=$!
stop_fulcio_proxy() {
  unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy NO_PROXY no_proxy
  if kill -0 "$fulcio_proxy_pid" 2>/dev/null; then
    kill -TERM "$fulcio_proxy_pid" 2>/dev/null || true
    attempts=0
    while kill -0 "$fulcio_proxy_pid" 2>/dev/null && [ "$attempts" -lt 50 ]; do
      sleep 0.1
      attempts=$((attempts + 1))
    done
    if kill -0 "$fulcio_proxy_pid" 2>/dev/null; then
      kill -KILL "$fulcio_proxy_pid" 2>/dev/null || true
    fi
  fi
  wait "$fulcio_proxy_pid" 2>/dev/null || true
  rm -f "$proxy_ready"
}
trap stop_fulcio_proxy EXIT INT TERM

attempts=0
while [ ! -s "$proxy_ready" ] && [ "$attempts" -lt 50 ]; do
  kill -0 "$fulcio_proxy_pid" 2>/dev/null || {
    echo "FAIL: Fulcio credential proxy exited before readiness"
    exit 1
  }
  sleep 0.1
  attempts=$((attempts + 1))
done
test -s "$proxy_ready"
fulcio_proxy_url="$(cat "$proxy_ready")"
case "$fulcio_proxy_url" in
  http://127.0.0.1:*) ;;
  *) echo "FAIL: credential proxy did not publish a loopback URL"; exit 1 ;;
esac
export HTTP_PROXY="$fulcio_proxy_url" HTTPS_PROXY="$fulcio_proxy_url"
export http_proxy="$fulcio_proxy_url" https_proxy="$fulcio_proxy_url"
export NO_PROXY='' no_proxy=''

curl -fsS "` + fulcioURL + `/api/v2/trustBundle" \
  | jq -r '.chains[0].certificates[]' > fulcio-root.pem
test -s fulcio-root.pem

cosign trusted-root create \
  --fulcio="url=` + fulcioURL + `,certificate-chain=fulcio-root.pem" \
  --no-default-fulcio --no-default-rekor --no-default-ctfe --no-default-tsa \
  --out trusted-root.json

mkdir -p dist
echo "par-sign-2 payload" > dist/artifact.tgz

sign_status=0
SIGSTORE_ID_TOKEN="$rc_sigstore_id_token" ./reusable-ci release sign \
  --method=sigstore \
  --release-artifacts-dir dist \
  --no-checksums-file \
  --oidc-issuer "` + oidcIssuer + `" \
  --fulcio-url "` + fulcioURL + `" \
  --trusted-root trusted-root.json > product-out.txt 2>&1 || sign_status=$?

unset SIGSTORE_ID_TOKEN rc_sigstore_id_token
if [ "$credential_trace" = 1 ]; then
  set -x
fi
cat product-out.txt
if [ "$sign_status" -ne 0 ] &&
   grep -qE 'usage error|flag provided but not defined|requires a subcommand|is required' product-out.txt; then
  echo "FAIL: the probe invoked the product incorrectly; this is a fixture bug, not a result"
  exit 1
fi
if [ "$sign_status" -ne 0 ]; then
  echo "FAIL: the product exited $sign_status; see its output above"
  exit "$sign_status"
fi

bundle="$(find . -maxdepth 2 -name '*.bundle' -type f | head -n 1)"
if [ -z "$bundle" ]; then
  echo "FAIL: signing reported success but produced no bundle"
  exit 1
fi

cosign verify-blob \
  --bundle "$bundle" \
  --certificate-oidc-issuer "` + oidcIssuer + `" \
  --certificate-identity-regexp "` + identity + `" \
  --trusted-root trusted-root.json \
  --insecure-ignore-tlog \
  --insecure-ignore-sct \
  dist/artifact.tgz

stop_fulcio_proxy
trap - EXIT INT TERM
echo "keyless signature verified against the lab CA"`
}

func indentProbe(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)

	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}
