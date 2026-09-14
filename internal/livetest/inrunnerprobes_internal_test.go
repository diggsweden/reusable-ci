// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"gopkg.in/yaml.v3"
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

// fixtureAsset is a probe asset whose digest is a well-formed placeholder, for
// renders that are inspected rather than executed.
func fixtureAsset(assetURL string) ProbeAsset {
	return ProbeAsset{URL: assetURL, SHA256: strings.Repeat("a", 64)}
}

func TestRegistryAuthProbe_NeverRendersCredentialValuesOrAuthFiles(t *testing.T) {
	t.Parallel()

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		target := probeTarget(t, forge)
		script := RegistryAuthProbe(target, "https://"+target.Host+"/asset", fixtureAsset("https://"+target.Host+"/credential-proxy"))
		encoded := base64.StdEncoding.EncodeToString([]byte(target.CredentialUsername + ":" + target.Token))

		for _, forbidden := range []string{target.Token, encoded, "cat auth.json", "echo \"$stored"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("%s generated registry probe contains secret-bearing diagnostic %q", forge, forbidden)
			}
		}

		if disableTrace := strings.Index(script, "credential_trace=0"); disableTrace < 0 ||
			strings.Index(script, "stored=\"") <= disableTrace ||
			!strings.Contains(script, "unset stored decoded username password") || !strings.Contains(script, "rm -f auth.json") {
			t.Errorf("%s generated registry probe does not suppress tracing around credential use", forge)
		}

		if strings.Contains(script, "WWW-Authenticate") || strings.Contains(script, "realm=\"") ||
			!strings.Contains(script, "buildah login") {
			t.Errorf("%s registry probe does not delegate authentication parsing to the OCI client", forge)
		}

		for _, authority := range target.RegistryAuthorities {
			if !strings.Contains(script, `--authority "`+authority+`"`) {
				t.Errorf("%s registry probe does not constrain OCI auth to %q", forge, authority)
			}
		}

		for _, unapproved := range target.OIDCAuthorities {
			if strings.Contains(script, `--authority "`+unapproved+`"`) {
				t.Errorf("%s registry probe approves unrelated OIDC authority %q", forge, unapproved)
			}
		}

		proxyAt := strings.Index(script, "./credential-proxy --authority")
		credentialAt := strings.Index(script, "password=${decoded#*:}")

		loginAt := strings.Index(script, "buildah login")
		if proxyAt < 0 || credentialAt <= proxyAt || loginAt <= credentialAt ||
			!strings.Contains(script, "export NO_PROXY='' no_proxy=''") {
			t.Errorf("%s registry credential/proxy ordering is unsafe", forge)
		}
	}
}

func TestInRunnerProbes_RenderValidYAML(t *testing.T) {
	t.Parallel()

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		target := probeTarget(t, forge)
		probes := map[string]string{
			"registry auth": RegistryAuthProbe(target, "https://"+target.Host+"/asset", fixtureAsset("https://"+target.Host+"/credential-proxy")),
			"keyless sign": KeylessSignProbe(target,
				"https://"+target.Host+"/asset",
				fixtureAsset("https://"+target.Host+"/credential-proxy"),
				fixtureAsset("https://"+target.Host+"/cosign.gz"),
				fixtureAsset("https://"+target.Host+"/probe-json"),
				target.FulcioURL,
				target.OIDCIssuer),
		}

		for name, body := range probes {
			var document yaml.Node
			if err := yaml.Unmarshal([]byte(body), &document); err != nil {
				t.Errorf("%s %s probe is invalid YAML: %v", forge, name, err)
			}
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
			cosignAssetURL := "https://fixture.compose.forgelab:8443/cosign.gz"
			jsonAssetURL := "https://fixture.compose.forgelab:8443/probe-json"
			proxy, cosign, probeJSON := fixtureAsset(proxyAssetURL), fixtureAsset(cosignAssetURL), fixtureAsset(jsonAssetURL)
			body := keylessSignScript(target, assetURL, proxy, cosign, probeJSON, fulcioURL, issuer)
			script := KeylessSignProbe(target, assetURL, proxy, cosign, probeJSON, fulcioURL, issuer)

			for _, required := range []string{
				"set +x",
				"./credential-proxy --authority \"" + target.OIDCAuthorities[0] + "\"",
				"export HTTP_PROXY=\"$fulcio_proxy_url\" HTTPS_PROXY=\"$fulcio_proxy_url\"",
				"SIGSTORE_ID_TOKEN=\"$rc_sigstore_id_token\" ./reusable-ci release sign",
				"kill -KILL \"$fulcio_proxy_pid\"",
				cosignAssetURL,
				jsonAssetURL,
			} {
				if !strings.Contains(script, required) {
					t.Errorf("generated keyless probe lacks %q", required)
				}
			}

			// The credential capture is the start of the script: tracing is off
			// and the runner's variables are moved and unset before the prelude
			// fetches or runs anything. It was once a single first line; the
			// setup now lives in its own embedded file, so the test checks the
			// property rather than the line.
			capture := map[provider.ForgeAPI][]string{
				provider.ForgeGitLab: {"set +x", "unset rc_sigstore_id_token", `rc_sigstore_id_token="${SIGSTORE_ID_TOKEN:-}"`, "unset SIGSTORE_ID_TOKEN"},
				provider.ForgeForgejo: {
					"set +x", `rc_actions_id_token_request_url="${ACTIONS_ID_TOKEN_REQUEST_URL:-}"`,
					`rc_actions_id_token_request_token="${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}"`, "unset ACTIONS_ID_TOKEN_REQUEST_URL ACTIONS_ID_TOKEN_REQUEST_TOKEN",
				},
			}[forge]

			firstFetch := strings.Index(body, "curl ")
			previous := -1

			for _, statement := range capture {
				at := strings.Index(body, statement)
				if at < 0 || at <= previous || firstFetch < 0 || at > firstFetch {
					t.Fatalf("%s credential capture %q is missing, out of order or after the first fetch (at=%d previous=%d fetch=%d)", forge, statement, at, previous, firstFetch)
				}

				previous = at
			}

			install := strings.Index(body, "curl -fsSL -o cosign.gz")
			proxyStart := strings.Index(body, "env -u SIGSTORE_ID_TOKEN")
			proxyEnable := strings.Index(body, "export HTTP_PROXY=\"$fulcio_proxy_url\"")
			productUse := strings.Index(body, "SIGSTORE_ID_TOKEN=\"$rc_sigstore_id_token\" ./reusable-ci release sign")

			if productUse < 0 {
				t.Fatal("generated keyless probe has no invocation-scoped product use")
			}

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

			if strings.Contains(script, "github.com/sigstore") || strings.Contains(script, "apk add") {
				t.Error("generated keyless probe downloads tools from a public host")
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

// TestKeylessSignProbe_ChildrenReceiveOnlyInvocationScopedCredential runs the
// whole generated keyless script, installation phase included, against owned
// fake tools: a curl that serves staged fixture assets, a gzipped cosign that
// is only reachable by decompressing the downloaded asset, and a proxy and
// product that each verify the exact environment they receive. A second run
// changes one byte of the staged cosign.gz after its digest was taken and
// requires the script to stop before anything is decompressed or run.
func TestKeylessSignProbe_ChildrenReceiveOnlyInvocationScopedCredential(t *testing.T) { //nolint:gocognit,maintidx // One controlled execution of the generated script per forge and asset state.
	const cleanEnvironment = `[ "${SIGSTORE_ID_TOKEN+x}" != x ] || exit 90
[ "${ACTIONS_ID_TOKEN_REQUEST_URL+x}" != x ] || exit 91
[ "${ACTIONS_ID_TOKEN_REQUEST_TOKEN+x}" != x ] || exit 92
[ "${rc_sigstore_id_token+x}" != x ] || exit 93
[ "${rc_actions_id_token_request_url+x}" != x ] || exit 94
[ "${rc_actions_id_token_request_token+x}" != x ] || exit 95
`

	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		for _, tampered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tampered=%t", forge, tampered), func(t *testing.T) {
				target := probeTarget(t, forge)
				work, assets, bin := t.TempDir(), t.TempDir(), t.TempDir()

				writeProbeExecutable(t, filepath.Join(bin, "curl"), `#!/bin/sh
set -eu
`+cleanEnvironment+`out=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) shift; out=$1 ;;
    -*) ;;
    *) url=$1 ;;
  esac
  shift
done
case "$url" in
  *audience=sigstore*) printf '%s\n' '{"value":"generated-signing-secret"}' ;;
  *api/v2/trustBundle*) printf '%s\n' '{"chains":[{"certificates":["fixture-root"]}]}' ;;
  *) [ -n "$out" ] && cp "`+assets+`/${url##*/}" "$out" ;;
esac
`)

				stage := func(name, body string) string {
					if err := os.WriteFile(filepath.Join(assets, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}

					return fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
				}

				probeJSON := stage("probe-json", `#!/bin/sh
set -eu
`+cleanEnvironment+`case "${1:-}" in
  id-token) printf '%s\n' 'generated-signing-secret' ;;
  fulcio-certificates) printf '%s\n' 'fixture-root' ;;
  *) exit 2 ;;
esac
`)

				var compressed bytes.Buffer

				writer := gzip.NewWriter(&compressed)
				if _, err := writer.Write([]byte(`#!/bin/sh
set -eu
` + cleanEnvironment + `: > installed-cosign-ran
case "${1:-}" in
  version) printf '%s\n' 'gitVersion fixture' ;;
esac
`)); err != nil {
					t.Fatal(err)
				}

				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}

				cosign := stage("cosign.gz", compressed.String())

				// The prelude checks the runner binary against RC_LIVE_RUNNER_SHA256
				// before running it. Pin the digest of this fixture, so the test
				// neither depends on nor is broken by the host's environment.
				product := `#!/bin/sh
set -eu
[ "${1:-}" != --version ] || { printf '%s\n' 'reusable-ci fixture'; exit 0; }
[ "${SIGSTORE_ID_TOKEN:-}" = generated-signing-secret ] || exit 96
[ "${ACTIONS_ID_TOKEN_REQUEST_URL+x}" != x ] || exit 97
[ "${ACTIONS_ID_TOKEN_REQUEST_TOKEN+x}" != x ] || exit 98
[ "${rc_sigstore_id_token+x}" != x ] || exit 99
[ "${rc_actions_id_token_request_url+x}" != x ] || exit 100
[ "${rc_actions_id_token_request_token+x}" != x ] || exit 101
: > exact-product-environment
: > dist/artifact.bundle
printf '%s\n' 'product invocation complete'
`
				t.Setenv(runnerBinarySHA256Env, stage("reusable-ci", product))

				proxy := stage("credential-proxy", `#!/bin/sh
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

				if tampered {
					staged := compressed.Bytes()
					staged[len(staged)-1] ^= 0xff

					if err := os.WriteFile(filepath.Join(assets, "cosign.gz"), staged, 0o600); err != nil {
						t.Fatal(err)
					}
				}

				asset := func(name, digest string) ProbeAsset {
					return ProbeAsset{URL: "https://" + target.Host + "/" + name, SHA256: digest}
				}

				body := keylessSignScript(target, "https://"+target.Host+"/reusable-ci",
					asset("credential-proxy", proxy), asset("cosign.gz", cosign), asset("probe-json", probeJSON),
					target.FulcioURL, target.OIDCIssuer)
				body = strings.ReplaceAll(body, LabCAPath, filepath.Join(work, "lab-ca.crt"))

				scriptPath := filepath.Join(work, "probe.sh")
				if err := os.WriteFile(scriptPath, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}

				environment := append(withoutProbeCredentials(os.Environ()),
					"PATH="+bin+":"+os.Getenv("PATH"),
					"LC_ALL=C",
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

				for _, secret := range []string{
					"generated-signing-secret",
					"forgejo-request-secret",
					"https://" + target.Host + "/token?job=1",
				} {
					if strings.Contains(string(output), secret) {
						t.Fatalf("generated keyless script exposed %s under xtrace:\n%s", secret, output)
					}
				}

				markers := []string{"installed-cosign-ran", "exact-product-environment", "exact-proxy-environment"}

				if tampered {
					if err == nil || !strings.Contains(string(output), "cosign.gz: FAILED") {
						t.Fatalf("a tampered cosign.gz did not stop the script at its digest check: %v\n%s", err, output)
					}

					for _, name := range append(markers, "cosign") {
						if _, statErr := os.Stat(filepath.Join(work, name)); !errors.Is(statErr, os.ErrNotExist) {
							t.Errorf("%s exists after the digest check refused cosign.gz: %v", name, statErr)
						}
					}

					return
				}

				if err != nil {
					t.Fatalf("generated keyless script failed: %v\n%s", err, output)
				}

				for _, marker := range markers {
					if _, err := os.Stat(filepath.Join(work, marker)); err != nil {
						t.Fatalf("%s was not verified: %v", marker, err)
					}
				}
			})
		}
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

// gitLabProbeJob and forgejoProbeWorkflow are the parsed shapes the generated
// probes must have: the structure, not strings that merely appear somewhere.
type gitLabProbeJob struct {
	Image    string `yaml:"image"`
	IDTokens map[string]struct {
		Aud string `yaml:"aud"`
	} `yaml:"id_tokens"`
	Script []string `yaml:"script"`
}

type forgejoProbeWorkflow struct {
	On                  []string `yaml:"on"`
	EnableOpenIDConnect bool     `yaml:"enable-openid-connect"`
	Jobs                map[string]struct {
		RunsOn    string `yaml:"runs-on"`
		Container *struct {
			Image string `yaml:"image"`
		} `yaml:"container"`
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// probeScript parses one generated probe and returns its only job's only
// script, requiring the job name, pinned image and OIDC declaration its kind
// needs.
func probeScript(t *testing.T, forge provider.ForgeAPI, body, job string, keyless bool) string {
	t.Helper()

	if forge == provider.ForgeGitLab {
		var jobs map[string]gitLabProbeJob
		if err := yaml.Unmarshal([]byte(body), &jobs); err != nil {
			t.Fatal(err)
		}

		parsed, ok := jobs[job]
		if len(jobs) != 1 || !ok || parsed.Image != ProbeImage || len(parsed.Script) != 1 {
			t.Fatalf("GitLab probe structure = %+v, want only job %q on %s with one script", jobs, job, ProbeImage)
		}

		wantTokens := 0
		if keyless {
			wantTokens = 1
		}

		if len(parsed.IDTokens) != wantTokens || (keyless && parsed.IDTokens["SIGSTORE_ID_TOKEN"].Aud != "sigstore") {
			t.Fatalf("GitLab probe id_tokens = %+v, want only SIGSTORE_ID_TOKEN for sigstore when keyless (%t)", parsed.IDTokens, keyless)
		}

		return parsed.Script[0]
	}

	var workflow forgejoProbeWorkflow
	if err := yaml.Unmarshal([]byte(body), &workflow); err != nil {
		t.Fatal(err)
	}

	parsed, ok := workflow.Jobs[job]
	if len(workflow.On) != 1 || workflow.On[0] != "push" || workflow.EnableOpenIDConnect != keyless ||
		len(workflow.Jobs) != 1 || !ok || parsed.RunsOn != "ubuntu-latest" || len(parsed.Steps) != 1 {
		t.Fatalf("Forgejo probe structure = %+v, want only job %q on push, OIDC %t, one step", workflow, job, keyless)
	}

	// The keyless job runs in the pinned image; the registry job logs in on the
	// runner host itself, which provides the forge's registry credential.
	if keyless != (parsed.Container != nil && parsed.Container.Image == ProbeImage) {
		t.Fatalf("Forgejo probe container = %+v, want %s exactly when keyless (%t)", parsed.Container, ProbeImage, keyless)
	}

	return parsed.Steps[0].Run
}

var authorityArgument = regexp.MustCompile(`--authority "([^"]*)"`)

// TestInRunnerProbes_StructureCarriesTheApprovedScript parses every generated
// probe: the one job's script is exactly the rendered script body, the
// credential proxy is given exactly the approved authorities in order, the
// shell authority guard admits exactly the approved origins, and each proxy
// trap is armed after the proxy starts, before the proxy is used, and disarmed
// only after its final stop.
func TestInRunnerProbes_StructureCarriesTheApprovedScript(t *testing.T) { //nolint:gocognit // One table of structural claims per probe kind.
	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		t.Run(string(forge), func(t *testing.T) {
			target := probeTarget(t, forge)
			asset, proxyAsset := "https://"+target.Host+"/reusable-ci", fixtureAsset("https://"+target.Host+"/credential-proxy")
			cosignAsset, jsonAsset := fixtureAsset("https://"+target.Host+"/cosign.gz"), fixtureAsset("https://"+target.Host+"/probe-json")

			keylessBody := keylessSignScript(target, asset, proxyAsset, cosignAsset, jsonAsset, target.FulcioURL, target.OIDCIssuer)
			keyless := probeScript(t, forge, KeylessSignProbe(target, asset, proxyAsset, cosignAsset, jsonAsset, target.FulcioURL, target.OIDCIssuer), "sign", true)

			// A YAML literal block keeps exactly one trailing newline.
			if keyless != strings.TrimRight(keylessBody, "\n")+"\n" {
				t.Fatalf("keyless job script is not exactly the rendered body")
			}

			registry := probeScript(t, forge, RegistryAuthProbe(target, asset, proxyAsset), "detect", false)
			verify := registryAuthVerify(target, proxyAsset)
			prelude := ProbePrelude(target, asset)

			preludeAt, verifyAt := strings.Index(registry, prelude), strings.Index(registry, verify)
			if preludeAt != 0 || verifyAt <= preludeAt || !strings.HasSuffix(registry, strings.TrimRight(verify, "\n")+"\n") {
				t.Fatalf("registry job script does not start with the prelude and end with the verification (prelude=%d verify=%d)", preludeAt, verifyAt)
			}

			for name, tc := range map[string]struct {
				script, guardVariable, stop string
				authorities, guard          []string
			}{
				"keyless": {script: keyless, stop: "stop_fulcio_proxy", authorities: target.OIDCAuthorities},
				"registry": {
					script: registry, stop: "stop_registry_proxy", authorities: target.RegistryAuthorities,
					guardVariable: "registry_url", guard: []string{`"` + target.RegistryOrigin + `"`},
				},
			} {
				var got []string
				for _, match := range authorityArgument.FindAllStringSubmatch(tc.script, -1) {
					got = append(got, match[1])
				}

				if !slices.Equal(got, tc.authorities) {
					t.Errorf("%s proxy authorities = %q, want exactly %q", name, got, tc.authorities)
				}

				if tc.guardVariable != "" {
					if entries := authorityGuardEntries(t, tc.script, tc.guardVariable); !slices.Equal(entries, tc.guard) {
						t.Errorf("%s authority guard admits %q, want exactly %q", name, entries, tc.guard)
					}
				}

				started := strings.Index(tc.script, "_proxy_pid=$!")
				armed := strings.Index(tc.script, "trap "+tc.stop+" EXIT INT TERM")
				used := strings.Index(tc.script, "export HTTP_PROXY=")
				finalStop := strings.LastIndex(tc.script, "\n"+tc.stop+"\n")
				disarmed := strings.LastIndex(tc.script, "trap - EXIT INT TERM")

				if started < 0 || armed <= started || used <= armed || finalStop <= used || disarmed <= finalStop ||
					strings.Count(tc.script, "trap "+tc.stop+" EXIT INT TERM") != 1 {
					t.Errorf("%s proxy trap lifecycle is unsafe: started=%d armed=%d used=%d stop=%d disarmed=%d", name, started, armed, used, finalStop, disarmed)
				}
			}

			if forge == provider.ForgeForgejo {
				if entries := authorityGuardEntries(t, keyless, "rc_actions_id_token_request_url"); !slices.Equal(entries,
					[]string{`"` + target.ForgeAuthorities[0] + `"|"` + target.ForgeAuthorities[0] + `/"*`}) {
					t.Errorf("Forgejo token request guard admits %q, want only the forge origin", entries)
				}
			}
		})
	}
}

// authorityGuardEntries returns the accepted patterns of the generated case
// statement on variable, without its refusing default.
func authorityGuardEntries(t *testing.T, script, variable string) []string {
	t.Helper()

	start := strings.Index(script, `case "$`+variable+`" in`)
	if start < 0 {
		t.Fatalf("no authority guard on %s", variable)
	}

	end := strings.Index(script[start:], "esac")
	if end < 0 {
		t.Fatalf("authority guard on %s is not closed", variable)
	}

	var entries []string

	for _, line := range strings.Split(script[start:start+end], "\n")[1:] {
		line = strings.TrimSpace(line)
		if pattern, ok := strings.CutSuffix(line, ") ;;"); ok {
			entries = append(entries, pattern)
		}
	}

	return entries
}

// TestInRunnerProbes_EveryDownloadedExecutableIsDigestCheckedFirst requires,
// for each executable a probe downloads, exactly one digest check naming the
// staged digest, after its download and before it is decompressed, marked
// executable or run. The rendered check itself is executed against the
// staged bytes and against the same file with one byte changed.
func TestInRunnerProbes_EveryDownloadedExecutableIsDigestCheckedFirst(t *testing.T) {
	for _, forge := range []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo} {
		t.Run(string(forge), func(t *testing.T) {
			target := probeTarget(t, forge)
			digest := func(name string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte("staged "+name))) }
			asset := func(name string) ProbeAsset {
				return ProbeAsset{URL: "https://" + target.Host + "/" + name, SHA256: digest(name)}
			}

			keyless := keylessSignScript(target, "https://"+target.Host+"/reusable-ci",
				asset("credential-proxy"), asset("cosign.gz"), asset("probe-json"), target.FulcioURL, target.OIDCIssuer)
			registry := registryAuthVerify(target, asset("credential-proxy"))

			for _, tc := range []struct {
				script, file, firstUse string
			}{
				{script: keyless, file: "cosign.gz", firstUse: "gzip -d cosign.gz"},
				{script: keyless, file: "probe-json", firstUse: "chmod +x probe-json"},
				{script: keyless, file: "credential-proxy", firstUse: "chmod +x credential-proxy"},
				{script: registry, file: "credential-proxy", firstUse: "chmod +x credential-proxy"},
			} {
				check := "printf '%s  " + tc.file + "\\n' '" + digest(tc.file) + "' | sha256sum --check --strict\n"

				download := strings.Index(tc.script, "curl -fsSL -o "+tc.file+" ")
				checked := strings.Index(tc.script, check)
				used := strings.Index(tc.script, tc.firstUse)

				if strings.Count(tc.script, check) != 1 || download < 0 || checked <= download || used <= checked {
					t.Errorf("%s is not digest-checked once between download and first use (download=%d check=%d use=%d)", tc.file, download, checked, used)

					continue
				}

				dir := t.TempDir()
				path := filepath.Join(dir, tc.file)

				for content, wantOK := range map[string]bool{"staged " + tc.file: true, "staged " + tc.file + "!": false} {
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}

					command := exec.CommandContext(t.Context(), "/bin/sh", "-c", check) //nolint:gosec // Fixed shell runs the rendered digest check.
					command.Dir = dir

					if output, err := command.CombinedOutput(); (err == nil) != wantOK {
						t.Errorf("%s digest check with %q: err = %v, want success %t\n%s", tc.file, content, err, wantOK, output)
					}
				}
			}
		})
	}

	for _, malformed := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a probe asset digest %q rendered", malformed)
				}
			}()

			_ = registryAuthVerify(probeTarget(t, provider.ForgeForgejo), ProbeAsset{URL: "https://fixture.invalid/credential-proxy", SHA256: malformed})
		}()
	}
}
