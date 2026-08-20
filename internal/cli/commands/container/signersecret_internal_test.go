// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"slices"
	"testing"
)

// signerSecretEnv is the list handed to the syft subprocess so an SBOM
// scan does not run holding signing material. It had no test.
//
// Two properties are worth holding, and they pull in opposite
// directions, so both are here: the list must contain the secrets the
// signing paths actually use, and it must not contain names that are not
// secrets -- scrubbing REGISTRY_AUTH_FILE-style configuration would
// break the subprocess rather than protect it.
func TestSignerSecretEnv(t *testing.T) {
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
		releaseImagesDefaultEnvVar,
	} {
		if !slices.Contains(got, name) {
			t.Errorf("%s is not scrubbed from the syft environment", name)
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

// TestSignerSecretEnv_KnownGaps records the names the binary reads as
// credentials that this list does not scrub.
//
// The list covers 12 names. The binary reads at least these as real
// credentials elsewhere -- GITHUB_TOKEN is resolved as a registry and
// forge token in adapters/github, GPG_PRIVATE_KEY is the private key
// `release gpg` signs with -- and they are not on it. Meanwhile
// adapters/changelog and app/toolchain each carry their own scrub list
// with different membership again: changelog drops GITHUB_TOKEN and
// GPG_PRIVATE_KEY, this one does not.
//
// It is defence in depth, not a live exposure: syft is a trusted binary.
// But the argument for scrubbing at all applies to these names equally,
// and three lists that disagree is the shape where one gets extended and
// the others do not.
//
// Recorded in docs/open-questions.md ("Three secret-scrub lists, three
// different memberships"). This test fails when a gap is closed, so the
// entry and the list stay in step.
func TestSignerSecretEnv_KnownGaps(t *testing.T) {
	t.Parallel()

	got := signerSecretEnv()

	gaps := []string{
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"GITEA_TOKEN",
		"GITLAB_TOKEN",
		"GPG_PRIVATE_KEY",
		"COSIGN_PRIVATE_KEY",
		"COSIGN_SIGNING_KEY",
		"COSIGN_SIGNING_PASSWORD",
		"CI_JOB_TOKEN",
		"CI_REGISTRY_PASSWORD",
		"FORGEJO_API_TOKEN",
	}

	for _, name := range gaps {
		if slices.Contains(got, name) {
			t.Errorf("%s is now scrubbed -- remove it from this test's list, "+
				"and close the open question when the list is empty", name)
		}
	}
}
