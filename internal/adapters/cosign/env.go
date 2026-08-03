// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import (
	"os"
	"strings"
)

// runtimeKeep is the set of environment variables a cosign subprocess needs
// regardless of signing mode: tool lookup (PATH), config/cache homes, the temp
// dir, and — because cosign reaches the network unless the transparency log is
// off — the TLS trust roots and the proxy configuration. Everything else is
// dropped under isolation unless explicitly allowed.
//
// Why the proxy vars are here. Keeping the TLS trust roots so cosign can reach
// Rekor while dropping the configuration that says HOW to reach it is half a
// network configuration: on a proxy-only network the signing step then fails at
// release time, and only for env:// keys, since those are the only ones that
// take the isolated path (see provenanceSignAllow). That asymmetry is invisible
// until it fires — a file key signs, an env:// key does not — and forgejo-ci
// signs with `--key env://COSIGN_KEY` in production. Both cases of each var are
// kept because Go's http.ProxyFromEnvironment honours either.
//
// The honest caveat: a proxy URL may embed credentials
// (http://user:pass@proxy), so unlike the rest of this set these are not
// guaranteed non-secret. That is accepted deliberately. The isolation exists to
// bound which secrets a cosign compromise could reach, and cosign already holds
// the signing key — the thing actually worth protecting. Withholding the proxy
// config buys no containment either, since cosign must reach Rekor by design on
// the public path; it only breaks the feature. Callers who want a signing run
// that genuinely touches no network should set transparency=none, which removes
// the reason to have a proxy at all.
//
//nolint:gochecknoglobals // read-only allow-set; a const map is not expressible in Go.
var runtimeKeep = map[string]bool{
	"PATH":            true,
	"HOME":            true,
	"TMPDIR":          true,
	"SSL_CERT_FILE":   true,
	"SSL_CERT_DIR":    true,
	"XDG_CONFIG_HOME": true,
	"XDG_CACHE_HOME":  true,
	"HTTP_PROXY":      true,
	"HTTPS_PROXY":     true,
	"NO_PROXY":        true,
	"http_proxy":      true,
	"https_proxy":     true,
	"no_proxy":        true,
}

// IsolatedEnv returns a minimal environment for a cosign subprocess: the
// runtimeKeep vars plus ONLY the named secret vars from the current
// process environment. It is the Go equivalent of forgejo-ci's
// `unset <other-secret-families>` before signing — a hardening so a
// cosign release cannot read forge tokens, GPG keys, or registry
// credentials it has no business seeing. The returned slice is suitable
// for exec.Cmd.Env.
func IsolatedEnv(allow ...string) []string {
	allowSet := make(map[string]bool, len(allow))
	for _, k := range allow {
		allowSet[k] = true
	}

	return filterEnv(os.Environ(), allowSet)
}

// filterEnv keeps each "KEY=VALUE" entry whose key is in runtimeKeep or
// allow, dropping everything else. Pure (operates on the passed slice)
// so the policy is testable without mutating the process environment.
func filterEnv(environ []string, allow map[string]bool) []string {
	out := make([]string, 0, len(environ))

	for _, kv := range environ {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}

		if runtimeKeep[key] || allow[key] {
			out = append(out, kv)
		}
	}

	return out
}

// NewIsolated returns an Adapter whose cosign subprocess sees only the
// runtime env plus the named secret vars (e.g. the COSIGN key family).
// Used by the provenance signer so an env-supplied signing key is the
// only secret cosign can read.
func NewIsolated(allow ...string) *Adapter {
	return &Adapter{
		Env:          IsolatedEnv(allow...),
		Transparency: TransparencyFromEnv(),
	}
}
