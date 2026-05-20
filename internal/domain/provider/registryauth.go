// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import "strings"

// RegistryAuth is the runner-injected credential for a forge's own container
// registry — the username/token a job already holds (GitHub's $GITHUB_TOKEN,
// GitLab's $CI_REGISTRY_PASSWORD, Forgejo's token) so `container login` can
// authenticate to the forge registry without a separately-managed secret.
type RegistryAuth struct {
	// Registry is the forge's native registry host (e.g. "ghcr.io",
	// "registry.gitlab.com", a Forgejo instance host). The caller uses it to
	// confirm the login target is this forge's registry before applying the
	// derived credentials — a runner token must not be sent to an unrelated
	// registry.
	Registry string
	Username string
	Token    string
}

// MatchesRegistry reports whether target is this forge's native registry, so
// the consumer only applies runner-injected credentials when logging in to the
// forge's own registry. An empty target means "the caller defaulted to the
// forge registry" and matches. Comparison is host-only and case-insensitive.
func (a RegistryAuth) MatchesRegistry(target string) bool {
	if target == "" {
		return true
	}

	return strings.EqualFold(registryHost(target), registryHost(a.Registry))
}

// registryHost strips a scheme and any path so only the host[:port] is
// compared (ghcr.io, registry.gitlab.com:443).
func registryHost(ref string) string {
	ref = strings.TrimPrefix(strings.TrimPrefix(ref, "https://"), "http://")
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		ref = ref[:i]
	}

	return ref
}

// RegistryAuthResolver is the provider role for forges that inject container-
// registry credentials into the job environment. GitHub, GitLab, and Forgejo
// implement it; the local forge does not, so deps.RequireRegistryAuthResolver
// returns the unsupported-role error and the caller falls back to explicit
// --username / --password.
//
// Resolution is pure environment reading (no network), matching the other
// forge resolver roles, so it needs no context.
type RegistryAuthResolver interface {
	// ResolveRegistryAuth returns the runner-injected registry credentials. It
	// errors (errs.RuntimeRequired) when the role is implemented but the
	// expected runner env is absent — i.e. not running inside a CI job.
	ResolveRegistryAuth() (RegistryAuth, error)
}
