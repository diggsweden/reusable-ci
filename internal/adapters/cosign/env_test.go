// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import (
	"slices"
	"testing"
)

func TestFilterEnv_KeepsRuntimeAndAllowedSecretsOnly(t *testing.T) {
	t.Parallel()

	environ := []string{
		"PATH=/usr/bin",                 // runtime → kept
		"HOME=/home/ci",                 // runtime → kept
		"COSIGN_KEY=-----BEGIN-----",    // allowed secret → kept
		"COSIGN_PASSWORD=hunter2",       // allowed secret → kept
		"FORGEJO_TOKEN=should-not-leak", // other secret → dropped
		"GPG_SIGNING_KEY=nope",          // other secret → dropped
		"REGISTRY_TOKEN=nope",           // other secret → dropped
		"malformed-no-equals",           // skipped
	}

	got := filterEnv(environ, map[string]bool{"COSIGN_KEY": true, "COSIGN_PASSWORD": true})

	want := []string{"PATH=/usr/bin", "HOME=/home/ci", "COSIGN_KEY=-----BEGIN-----", "COSIGN_PASSWORD=hunter2"}
	if !slices.Equal(got, want) {
		t.Errorf("filterEnv kept %v, want %v", got, want)
	}

	for _, kv := range got {
		if kv == "FORGEJO_TOKEN=should-not-leak" || kv == "GPG_SIGNING_KEY=nope" || kv == "REGISTRY_TOKEN=nope" {
			t.Errorf("secret leaked into isolated env: %q", kv)
		}
	}
}

// TestIsolationKeepsTheNetworkConfigurationWhole pins the pairing that makes
// isolated signing work on a proxy-only network. cosign must reach Rekor on the
// public path — that is why the TLS trust roots are kept — so dropping the
// proxy configuration while keeping them leaves half a network configuration,
// and the signing step fails at release time.
//
// It fails ONLY for env:// keys, because those are the only ones that take the
// isolated path (provenanceSignAllow), so a file key would sign happily while
// an env:// key did not. forgejo-ci signs with `--key env://COSIGN_KEY`, which
// is exactly the affected case.
//
// Both cases of each var are kept: Go's http.ProxyFromEnvironment honours
// either spelling, so keeping only one would work by accident of which the
// operator happened to set.
func TestIsolationKeepsTheNetworkConfigurationWhole(t *testing.T) {
	t.Parallel()

	network := []string{
		"SSL_CERT_FILE=/etc/ssl/ca.pem", // how to trust the endpoint
		"SSL_CERT_DIR=/etc/ssl/certs",
		"HTTP_PROXY=http://proxy:8080", // how to REACH the endpoint
		"HTTPS_PROXY=http://proxy:8080",
		"NO_PROXY=internal.example",
		"http_proxy=http://proxy:8080",
		"https_proxy=http://proxy:8080",
		"no_proxy=internal.example",
	}

	got := filterEnv(append(slices.Clone(network), "FORGEJO_TOKEN=nope"), map[string]bool{})

	for _, kv := range network {
		if !slices.Contains(got, kv) {
			t.Errorf("isolation dropped %q — cosign cannot reach Rekor without it, so an "+
				"env:// key would fail to sign on a proxy-only network while a file key succeeded", kv)
		}
	}

	// The point of isolation is unchanged: this widens the network config, not
	// the secret surface.
	if slices.Contains(got, "FORGEJO_TOKEN=nope") {
		t.Error("isolation leaked an unrelated secret")
	}
}

func TestNewIsolated_SetsEnv(t *testing.T) {
	// No t.Parallel(): t.Setenv is incompatible with parallel tests.
	t.Setenv("COSIGN_KEY", "k")
	t.Setenv("SOME_OTHER_SECRET", "s")

	a := NewIsolated("COSIGN_KEY")
	if a.Env == nil {
		t.Fatal("NewIsolated should set Env")
	}

	for _, kv := range a.Env {
		if kv == "SOME_OTHER_SECRET=s" {
			t.Errorf("non-allowed secret present in isolated env: %q", kv)
		}
	}
}

func TestNew_InheritsEnv(t *testing.T) {
	t.Parallel()

	// Default adapter leaves Env nil → inherit (backwards compatible).
	if New().Env != nil {
		t.Error("New() must leave Env nil so the subprocess inherits the parent env")
	}
}
