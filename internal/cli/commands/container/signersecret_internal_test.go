// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// signerSecretEnv is the list handed to the syft and skopeo subprocesses
// so neither an SBOM scan nor a registry copy runs holding signing
// material.
//
// Two properties are worth holding, and they pull in opposite
// directions, so both are here: the list must contain the secrets the
// signing paths actually use, and it must not contain names that are not
// secrets -- scrubbing REGISTRY_AUTH_FILE-style configuration would
// break the subprocess rather than protect it.
func TestSignerSecretEnv_ScrubsEveryCredentialWithoutDuplicates(t *testing.T) {
	t.Parallel()

	got := signerSecretEnv()

	// The signing key material and its passphrases: the reason the list
	// exists. A scan has no use for any of these.
	for _, name := range []string{
		"COSIGN_KEY",
		"COSIGN_PASSWORD",
		"GPG_SIGNING_KEY",
		"GPG_SIGNING_PASSWORD",
		"REGISTRY_PASSWORD",
		"REGISTRY_TOKEN",

		// Names the binary reads as real credentials elsewhere, which
		// this list did not cover: GITHUB_TOKEN is resolved as both a
		// registry and a forge token in adapters/github, and
		// GPG_PRIVATE_KEY is the key `release gpg` signs with. They
		// were listed as known gaps until this list caught up.
		"CI_JOB_TOKEN",
		"CI_REGISTRY_PASSWORD",
		"COSIGN_PRIVATE_KEY",
		"COSIGN_SIGNING_KEY",
		"COSIGN_SIGNING_PASSWORD",
		"FORGEJO_API_TOKEN",
		"GH_TOKEN",
		"GITEA_TOKEN",
		"GITHUB_TOKEN",
		"GITLAB_TOKEN",
		"GPG_PASSPHRASE",
		"GPG_PRIVATE_KEY",
	} {
		if !slices.Contains(got, name) {
			t.Errorf("%s is not scrubbed from the syft environment", name)
		}
	}

	for _, name := range []string{"REGISTRY_AUTH_FILE", "DOCKER_CONFIG", "CI_REGISTRY", "CI_REGISTRY_IMAGE", "GITHUB_REPOSITORY", "SSL_CERT_FILE", "COSIGN_FULCIO_URL", "COSIGN_REKOR_URL", "PATH", "HOME"} {
		if slices.Contains(got, name) {
			t.Errorf("nonsecret configuration %s must remain available", name)
		}
	}

	// No duplicates and no empties: both would be silent, and an empty
	// entry means a constant that lost its value.
	seen := map[string]bool{}

	for _, name := range got {
		if name == "" {
			t.Error("the scrub list contains an empty name")
		}

		if seen[name] {
			t.Errorf("%s appears twice in the scrub list", name)
		}

		seen[name] = true
	}
}

// TestNewImageEvidenceSkopeo_ScrubsSigningMaterial covers the wiring,
// not the scrubbing.
//
// The skopeo adapter has always been able to drop variables; what was
// missing was a caller doing it. Both constructor paths are checked,
// because the auth-file branch replaces the adapter wholesale and is the
// one a registry-talking run actually takes.
func TestNewImageEvidenceSkopeo_ScrubsSigningMaterial(t *testing.T) {
	t.Parallel()

	for _, authFile := range []string{"", "/run/containers/auth.json"} {
		adapter := newImageEvidenceSkopeo(authFile)

		if adapter.AuthFile != authFile {
			t.Errorf("AuthFile = %q, want %q", adapter.AuthFile, authFile)
		}

		if !slices.Equal(adapter.UnsetEnv, signerSecretEnv()) {
			t.Errorf("skopeo runs with UnsetEnv %v, want the signer scrub list", adapter.UnsetEnv)
		}
	}
}

// TestSignerSecretEnv_CoversEveryCredentialTheBinaryResolves is the check the
// hand-written expectations above cannot make.
//
// Those lists say "these names are secrets", which is true and was verified by
// a human reading them. What no assertion covered is the direction that
// actually goes wrong over time: a credential name added to runcontext — a new
// forge's token, a renamed one — and never added here. Both lists in this file
// would still pass, because both describe what someone already thought of.
//
// runcontext is where the binary declares what it treats as a credential, so it
// is the authority. Every name it resolves must be scrubbed before syft or
// skopeo runs, or a subprocess that has no use for signing material inherits it.
func TestSignerSecretEnv_CoversEveryCredentialTheBinaryResolves(t *testing.T) {
	t.Parallel()

	scrubbed := map[string]bool{}
	for _, name := range signerSecretEnv() {
		scrubbed[name] = true
	}

	declared := map[string]bool{}
	credentialVars := [][]string{runcontext.Token().Keys(), runcontext.ReleaseToken().Keys()}

	for _, keys := range credentialVars {
		for _, key := range keys {
			declared[key] = true
		}
	}

	if len(declared) == 0 {
		t.Fatal("runcontext declared no credential names; the accessor, not the scrub list, is what was measured")
	}

	missing := make([]string, 0, len(declared))
	for key := range declared {
		if !scrubbed[key] {
			missing = append(missing, key)
		}
	}

	slices.Sort(missing)

	if len(missing) > 0 {
		t.Errorf("runcontext resolves %v as credentials, but signerSecretEnv does not scrub them; "+
			"syft and skopeo would run holding them. Add them to signerSecretEnv, or say in runcontext why they "+
			"are not secrets.", missing)
	}
}
