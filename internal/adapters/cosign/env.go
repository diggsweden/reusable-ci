// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import (
	"os"
	"strings"
)

// runtimeKeep is the set of non-secret environment variables a cosign
// subprocess needs regardless of signing mode: tool lookup (PATH),
// config/cache homes, the temp dir, and the TLS trust roots (cosign
// uploads to Rekor by default). Everything else is dropped under
// isolation unless explicitly allowed.
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
	return &Adapter{Env: IsolatedEnv(allow...)}
}
