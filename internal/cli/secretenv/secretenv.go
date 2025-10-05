// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package secretenv is the one list of environment variables the binary reads
// as credentials. Commands hand it to adapters that run tools with no use for
// a secret (syft, skopeo, the changelog renderers), so those subprocesses do
// not inherit signing material or forge tokens. Keeping one list, reconciled
// against runcontext by its test, is what stops a second copy drifting.
package secretenv

// Names returns the credential variable names to remove from a subprocess
// environment. The slice is fresh on every call.
func Names() []string {
	return []string{
		"CI_JOB_TOKEN",
		"CI_REGISTRY_PASSWORD",
		"CI_TOKEN",
		"COSIGN_KEY",
		"COSIGN_PASSWORD",
		"COSIGN_PRIVATE_KEY",
		"COSIGN_SIGNING_KEY",
		"COSIGN_SIGNING_PASSWORD",
		"FORGEJO_API_TOKEN",
		"FORGEJO_TOKEN",
		"GH_TOKEN",
		"GITEA_TOKEN",
		"GITHUB_TOKEN",
		"GITLAB_TOKEN",
		"GPG_PASSPHRASE",
		"GPG_PRIVATE_KEY",
		"GPG_SIGNING_FINGERPRINT",
		"GPG_SIGNING_KEY",
		"GPG_SIGNING_PASSWORD",
		"MISE_FORGEJO_TOKEN",
		"MISE_GITHUB_TOKEN",
		"REGISTRY_PASSWORD",
		"REGISTRY_TOKEN",
		"REGISTRY_USER",
		"RELEASE_TOKEN",
		"REUSABLE_CI_PROVIDER_TOKEN",
		"SSH_SIGNING_KEY",
	}
}
