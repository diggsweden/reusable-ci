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

	// Membership, not order. These keys are unique, and for unique keys the
	// position in a process environment carries no meaning, so pinning the
	// order would fail a correct reimplementation. Order is asserted where it
	// is contractual -- duplicate keys -- in the test below.
	want := []string{"PATH=/usr/bin", "HOME=/home/ci", "COSIGN_KEY=-----BEGIN-----", "COSIGN_PASSWORD=hunter2"}
	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
		t.Errorf("filterEnv kept %v, want exactly %v in any order", got, want)
	}

	for _, kv := range got {
		if kv == "FORGEJO_TOKEN=should-not-leak" || kv == "GPG_SIGNING_KEY=nope" || kv == "REGISTRY_TOKEN=nope" {
			t.Errorf("secret leaked into isolated env: %q", kv)
		}
	}
}

// TestFilterEnv_PreservesTheOrderOfDuplicateKeys is where order is contractual.
//
// When a key appears twice, os/exec passes both and the last occurrence is the
// value the child sees. A filter that reordered surviving entries would silently
// swap which value wins -- for COSIGN_PASSWORD that is the difference between
// the key decrypting and the signing step failing.
func TestFilterEnv_PreservesTheOrderOfDuplicateKeys(t *testing.T) {
	t.Parallel()

	environ := []string{
		"COSIGN_PASSWORD=stale",
		"FORGEJO_TOKEN=dropped",
		"COSIGN_PASSWORD=current",
	}

	got := filterEnv(environ, map[string]bool{"COSIGN_PASSWORD": true})

	if want := []string{"COSIGN_PASSWORD=stale", "COSIGN_PASSWORD=current"}; !slices.Equal(got, want) {
		t.Errorf("filterEnv = %v, want %v with the later value still last", got, want)
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

// TestNewIsolated_KeepsTheAllowedKeyAndDropsTheRest asserts both halves.
//
// It used to check only that an unrelated secret was absent, which a
// NewIsolated keeping *nothing* satisfies -- verified: replacing the env with
// an empty slice left this test green. That is the more expensive direction to
// get wrong. A leak is caught in review; a signing key silently dropped from
// the subprocess environment fails at release time, which is precisely what
// TestIsolationKeepsTheNetworkConfigurationWhole below exists to prevent for
// the network half.
func TestNewIsolated_KeepsTheAllowedKeyAndDropsTheRest(t *testing.T) {
	// No t.Parallel(): t.Setenv is incompatible with parallel tests.
	t.Setenv("COSIGN_KEY", "k")
	t.Setenv("SOME_OTHER_SECRET", "s")

	a := NewIsolated("COSIGN_KEY")
	if a.Env == nil {
		t.Fatal("NewIsolated should set Env")
	}

	if !slices.Contains(a.Env, "COSIGN_KEY=k") {
		t.Errorf("the allowed key did not survive isolation, so cosign has nothing to sign with: %v", a.Env)
	}

	for _, kv := range a.Env {
		if kv == "SOME_OTHER_SECRET=s" {
			t.Errorf("non-allowed secret present in isolated env: %q", kv)
		}
	}
}

// TestForKeyRef_IsolatesOnlyWhatItCanIsolateSafely covers the rule three
// signing call sites used to spell out for themselves.
//
// It asserts the resulting environment rather than an intermediate
// allow-list, because the environment is what the subprocess actually
// gets. The negative half matters as much as the positive one: a KMS or
// keyless ref needs credential material this package cannot enumerate,
// so isolating it on a guess would break signing at release time rather
// than at review time.
func TestForKeyRef_IsolatesOnlyWhatItCanIsolateSafely(t *testing.T) {
	// No t.Parallel(): t.Setenv is incompatible with parallel tests.
	t.Setenv("MY_SIGNING_KEY", "k")
	t.Setenv("COSIGN_PASSWORD", "p")
	t.Setenv("DOCKER_CONFIG", "/run/auth.json")
	t.Setenv("GPG_PRIVATE_KEY", "leak-me")

	t.Run("env ref keeps its own key, the passphrase and the named extras", func(t *testing.T) {
		env := ForKeyRef("env://MY_SIGNING_KEY", "DOCKER_CONFIG").Env
		if env == nil {
			t.Fatal("an env:// key must produce an isolated environment")
		}

		for _, want := range []string{"MY_SIGNING_KEY=k", "COSIGN_PASSWORD=p", "DOCKER_CONFIG=/run/auth.json"} {
			if !slices.Contains(env, want) {
				t.Errorf("isolated env is missing %q: %v", want, env)
			}
		}

		if slices.Contains(env, "GPG_PRIVATE_KEY=leak-me") {
			t.Errorf("a secret cosign has no use for reached the subprocess: %v", env)
		}
	})

	for _, keyRef := range []string{
		"hashivault://transit/keys/release",
		"/keys/cosign.key",
		"env://",
		"",
	} {
		t.Run("ambient for "+keyRef, func(t *testing.T) {
			if env := ForKeyRef(keyRef, "DOCKER_CONFIG").Env; env != nil {
				t.Errorf("ForKeyRef(%q) isolated an environment it cannot enumerate: %v", keyRef, env)
			}
		})
	}
}

func TestNew_InheritsEnv(t *testing.T) {
	t.Parallel()

	// Default adapter leaves Env nil → inherit (backwards compatible).
	if New().Env != nil {
		t.Error("New() must leave Env nil so the subprocess inherits the parent env")
	}
}
