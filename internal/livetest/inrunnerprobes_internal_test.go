// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func probeTarget(t *testing.T, forge provider.ForgeAPI) Target {
	t.Helper()

	host := string(forge) + ".compose.forgelab:8443"
	registry := "https://" + host

	registryAuthorities := []string{registry}
	if forge == provider.ForgeGitLab {
		registry = "https://registry.gitlab.compose.forgelab:8443"
		registryAuthorities = []string{registry, "https://" + host}
	}

	return Target{
		Forge:               forge,
		Host:                host,
		Owner:               "fixture-user",
		Token:               "registry-secret-value",
		CredentialUsername:  "fixture-user",
		RegistryOrigin:      registry,
		ForgeAuthorities:    []string{"https://" + host},
		RegistryAuthorities: registryAuthorities,
		OIDCAuthorities:     []string{"https://fulcio.compose.forgelab:8443"},
		CAFile:              "/frozen/ca.pem",
		CAFacts:             "fixture-facts",
		FulcioURL:           "https://fulcio.compose.forgelab:8443",
		OIDCIssuer:          "https://" + host,
		caPEM:               independentCAPEM(t),
		accepted:            true,
	}
}

func TestRegistryAuthProbe_NeverRendersCredentialValuesOrAuthFiles(t *testing.T) {
	t.Parallel()

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		target := probeTarget(t, forge)
		script := RegistryAuthProbe(target, "https://"+target.Host+"/asset")
		encoded := base64.StdEncoding.EncodeToString([]byte(target.CredentialUsername + ":" + target.Token))

		for _, forbidden := range []string{target.Token, encoded, "cat auth.json", "echo \"$stored", "printf '%s' \"$stored"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("%s generated registry probe contains secret-bearing diagnostic %q", forge, forbidden)
			}
		}

		if disableTrace := strings.Index(script, "credential_trace=0"); disableTrace < 0 ||
			strings.Index(script, "stored=\"") <= disableTrace ||
			!strings.Contains(script, "unset stored token") || !strings.Contains(script, "rm -f auth.json") {
			t.Errorf("%s generated registry probe does not suppress tracing around credential use", forge)
		}
	}
}

func TestKeylessSignProbe_CapturesAndScopesRunnerCredentials(t *testing.T) { //nolint:gocognit // Each branch asserts one ordered credential boundary.
	t.Parallel()

	const (
		assetURL      = "https://fixture.compose.forgelab:8443/reusable-ci"
		proxyAssetURL = "https://fixture.compose.forgelab:8443/credential-proxy"
		fulcioURL     = "https://fulcio.compose.forgelab:8443"
	)

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		t.Run(string(forge), func(t *testing.T) {
			t.Parallel()

			target := probeTarget(t, forge)
			issuer := "https://" + target.Host
			body := keylessSignScript(target, assetURL, proxyAssetURL, fulcioURL, issuer)
			script := KeylessSignProbe(target, assetURL, proxyAssetURL, fulcioURL, issuer)
			firstLine := strings.SplitN(body, "\n", 2)[0]

			for _, required := range []string{
				"set +x",
				"./credential-proxy --authority \"" + target.OIDCAuthorities[0] + "\"",
				"export HTTP_PROXY=\"$fulcio_proxy_url\" HTTPS_PROXY=\"$fulcio_proxy_url\"",
				"SIGSTORE_ID_TOKEN=\"$rc_sigstore_id_token\" ./reusable-ci release sign",
				"kill -KILL \"$fulcio_proxy_pid\"",
			} {
				if !strings.Contains(script, required) {
					t.Errorf("generated keyless probe lacks %q", required)
				}
			}

			if forge == provider.ForgeGitLab &&
				(!strings.Contains(firstLine, "unset rc_sigstore_id_token") ||
					!strings.Contains(firstLine, `rc_sigstore_id_token="${SIGSTORE_ID_TOKEN:-}"`) ||
					!strings.Contains(firstLine, "unset SIGSTORE_ID_TOKEN")) {
				t.Fatalf("GitLab first script line does not capture its id_token: %s", firstLine)
			}
			if forge == provider.ForgeForgejo &&
				(!strings.Contains(firstLine, `rc_actions_id_token_request_url="${ACTIONS_ID_TOKEN_REQUEST_URL:-}"`) ||
					!strings.Contains(firstLine, `rc_actions_id_token_request_token="${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}"`) ||
					!strings.Contains(firstLine, "unset ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN")) {
				t.Fatalf("Forgejo first script line does not contain its request credentials: %s", firstLine)
			}

			install := strings.Index(body, "apk add")
			proxyStart := strings.Index(body, "env -u SIGSTORE_ID_TOKEN")
			proxyEnable := strings.Index(body, "export HTTP_PROXY=\"$fulcio_proxy_url\"")
			productUse := strings.Index(body, "SIGSTORE_ID_TOKEN=\"$rc_sigstore_id_token\" ./reusable-ci release sign")
			clearToken := strings.Index(body[productUse:], "unset SIGSTORE_ID_TOKEN rc_sigstore_id_token")
			traceRestore := strings.Index(body[productUse:], "set -x")
			if install <= 0 || proxyStart <= install || proxyEnable <= proxyStart || productUse <= proxyEnable ||
				clearToken < 0 || traceRestore <= clearToken {
				t.Fatalf("credential/proxy/product ordering is unsafe: install=%d proxy=%d enable=%d product=%d clear=%d trace=%d",
					install, proxyStart, proxyEnable, productUse, clearToken, traceRestore)
			}

			for _, removed := range []string{"export SIGSTORE_ID_TOKEN", "run_product release sign"} {
				if strings.Contains(body, removed) {
					t.Errorf("generated keyless probe contains broadly scoped token use %q", removed)
				}
			}

			if forge == provider.ForgeForgejo {
				authorityGuard := strings.Index(body, `case "$rc_actions_id_token_request_url" in`)
				tokenFetch := strings.Index(body, "--location --max-redirs 0")
				requestClear := strings.Index(body, "unset rc_token_response rc_actions_id_token_request_url rc_actions_id_token_request_token")
				if authorityGuard <= install || tokenFetch <= authorityGuard || requestClear <= tokenFetch || proxyStart <= requestClear {
					t.Fatalf("Forgejo token mint ordering is unsafe: guard=%d fetch=%d clear=%d proxy=%d",
						authorityGuard, tokenFetch, requestClear, proxyStart)
				}
			}

			encoded := base64.StdEncoding.EncodeToString([]byte(target.CredentialUsername + ":" + target.Token))
			for _, forbidden := range []string{target.Token, encoded, "cat auth.json", "head -c 300"} {
				if strings.Contains(script, forbidden) {
					t.Errorf("generated keyless probe contains secret-bearing output %q", forbidden)
				}
			}
		})
	}
}

func TestKeylessSignProbe_ChildrenReceiveOnlyInvocationScopedCredential(t *testing.T) {
	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		t.Run(string(forge), func(t *testing.T) {
			target := probeTarget(t, forge)
			work := t.TempDir()
			bin := filepath.Join(work, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}

			const cleanEnvironment = `[ "${SIGSTORE_ID_TOKEN+x}" != x ] || exit 90
[ "${ACTIONS_ID_TOKEN_REQUEST_URL+x}" != x ] || exit 91
[ "${ACTIONS_ID_TOKEN_REQUEST_TOKEN+x}" != x ] || exit 92
[ "${rc_sigstore_id_token+x}" != x ] || exit 93
[ "${rc_actions_id_token_request_url+x}" != x ] || exit 94
[ "${rc_actions_id_token_request_token+x}" != x ] || exit 95
`
			writeProbeExecutable(t, filepath.Join(bin, "apk"), "#!/bin/sh\nset -eu\n"+cleanEnvironment)
			writeProbeExecutable(t, filepath.Join(bin, "curl"), `#!/bin/sh
set -eu
`+cleanEnvironment+`case "$*" in
  *audience=sigstore*) printf '%s\n' '{"value":"generated-signing-secret"}' ;;
  *api/v2/trustBundle*) printf '%s\n' '{"chains":[{"certificates":["fixture-root"]}]}' ;;
esac
`)
			writeProbeExecutable(t, filepath.Join(bin, "jq"), `#!/bin/sh
set -eu
`+cleanEnvironment+`case "$*" in
  *'.value'*) printf '%s\n' 'generated-signing-secret' ;;
  *certificates*) printf '%s\n' 'fixture-root' ;;
  *) exit 2 ;;
esac
`)
			writeProbeExecutable(t, filepath.Join(bin, "cosign"), `#!/bin/sh
set -eu
`+cleanEnvironment+`case "${1:-}" in
  version) printf '%s\n' 'gitVersion fixture' ;;
esac
`)
			writeProbeExecutable(t, filepath.Join(work, "reusable-ci"), `#!/bin/sh
set -eu
[ "${SIGSTORE_ID_TOKEN:-}" = generated-signing-secret ] || exit 96
[ "${ACTIONS_ID_TOKEN_REQUEST_URL+x}" != x ] || exit 97
[ "${ACTIONS_ID_TOKEN_REQUEST_TOKEN+x}" != x ] || exit 98
[ "${rc_sigstore_id_token+x}" != x ] || exit 99
[ "${rc_actions_id_token_request_url+x}" != x ] || exit 100
[ "${rc_actions_id_token_request_token+x}" != x ] || exit 101
: > exact-product-environment
: > dist/artifact.bundle
printf '%s\n' 'product invocation complete'
`)
			writeProbeExecutable(t, filepath.Join(work, "credential-proxy"), `#!/bin/sh
set -eu
`+cleanEnvironment+`ready=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --ready-file ]; then shift; ready=$1; fi
  shift
done
[ -n "$ready" ]
: > exact-proxy-environment
printf '%s\n' 'http://127.0.0.1:43123' > "$ready"
trap 'exit 0' INT TERM
while :; do sleep 1; done
`)

			downloadedCosign := filepath.Join(work, "downloaded-cosign")
			writeProbeExecutable(t, downloadedCosign, "#!/bin/sh\nexit 0\n")
			body := keylessSignScript(target,
				"https://"+target.Host+"/reusable-ci",
				"https://"+target.Host+"/credential-proxy",
				target.FulcioURL,
				target.OIDCIssuer)
			body = strings.ReplaceAll(body, "/usr/local/bin/cosign", downloadedCosign)
			body = strings.ReplaceAll(body, LabCAPath, filepath.Join(work, "lab-ca.crt"))
			scriptPath := filepath.Join(work, "probe.sh")
			if err := os.WriteFile(scriptPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			environment := withoutProbeCredentials(os.Environ())
			environment = append(environment,
				"PATH="+bin+":"+os.Getenv("PATH"),
				"CI_PROJECT_PATH=fixture/project",
				"GITHUB_REPOSITORY=fixture/project",
			)
			if forge == provider.ForgeGitLab {
				environment = append(environment, "SIGSTORE_ID_TOKEN=generated-signing-secret")
			} else {
				environment = append(environment,
					"ACTIONS_ID_TOKEN_REQUEST_URL=https://"+target.Host+"/token?job=1",
					"ACTIONS_ID_TOKEN_REQUEST_TOKEN=forgejo-request-secret",
				)
			}

			command := exec.CommandContext(t.Context(), "/bin/sh", "-ex", scriptPath) //nolint:gosec // Fixed shell executes the test-authored generated fixture.
			command.Dir = work
			command.Env = environment
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("generated keyless script failed: %v\n%s", err, output)
			}
			for _, secret := range []string{
				"generated-signing-secret",
				"forgejo-request-secret",
				"https://" + target.Host + "/token?job=1",
			} {
				if strings.Contains(string(output), secret) {
					t.Fatalf("generated keyless script exposed %s under xtrace:\n%s", secret, output)
				}
			}
			for _, marker := range []string{"exact-product-environment", "exact-proxy-environment"} {
				if _, err := os.Stat(filepath.Join(work, marker)); err != nil {
					t.Fatalf("%s was not verified: %v", marker, err)
				}
			}
		})
	}
}

func writeProbeExecutable(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // Test fixtures must be executable.
		t.Fatal(err)
	}
}

func withoutProbeCredentials(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "SIGSTORE_ID_TOKEN", "ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
			"rc_sigstore_id_token", "rc_actions_id_token_request_url", "rc_actions_id_token_request_token":
			continue
		default:
			clean = append(clean, entry)
		}
	}

	return clean
}
