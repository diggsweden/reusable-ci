// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"fmt"
	"net/url"
	"strings"
)

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

// String redacts the token so an ordinary %v or %s cannot print it.
//
// RegistryAuth carries a live registry credential and is passed between
// adapters, so it reaches the places values get formatted by accident: a
// wrapped error, a debug line, a %+v on a struct that happens to embed it.
// runcontext.Credential has redacted formatting for exactly this reason; this
// type held the same class of secret in a plain field and did not.
//
// Registry and Username stay visible. They are not secret, and a diagnostic
// that cannot say WHICH registry a login was attempted against is the kind of
// redaction that gets removed again the first time someone debugs a failure.
func (a RegistryAuth) String() string {
	return fmt.Sprintf("provider.RegistryAuth{Registry:%q, Username:%q, Token:%s}",
		a.Registry, a.Username, redactedToken(a.Token))
}

// GoString redacts under %#v too, which is what a struct dump reaches for.
func (a RegistryAuth) GoString() string { return a.String() }

// redactedToken reports whether a token is present without revealing it.
// "absent" and "redacted" are different facts and a caller debugging an empty
// credential needs to tell them apart.
func redactedToken(token string) string {
	if token == "" {
		return "absent"
	}

	return "REDACTED"
}

// MatchesRegistry reports whether target is this forge's native registry, so
// the consumer only applies runner-injected credentials when logging in to the
// forge's own registry. An empty target means "the caller defaulted to the
// forge registry" and matches. Comparison is host-only and case-insensitive.
func (a RegistryAuth) MatchesRegistry(target string) bool {
	if target == "" {
		return true
	}

	targetHost := registryHost(target)
	authHost := registryHost(a.Registry)

	return targetHost != "" && authHost != "" && strings.EqualFold(targetHost, authHost)
}

// registryHost strips a scheme and any path so only the host[:port] is
// compared (ghcr.io, registry.gitlab.com:443).
func registryHost(ref string) string {
	parsed, err := parseRegistryURL(ref)
	if err != nil {
		return ""
	}

	return parsed.Host
}

func parseRegistryURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	return url.Parse(raw)
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
