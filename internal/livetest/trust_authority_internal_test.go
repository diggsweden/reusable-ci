// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The authority decisions below need no socket: they are pure functions of a
// URL and the approved set, and the delegation boundary is observed through a
// recording in-memory transport. The loopback TLS tests in
// trust_internal_test.go keep the wire behaviour (CONNECT, real redirects,
// bearer challenges through go-containerregistry).

func TestCredentialAuthorityKey_NormalizesOriginsAndRefusesEverythingElse(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"https://forge.example.test":          "forge.example.test:443",
		"https://forge.example.test:443":      "forge.example.test:443",
		"https://FORGE.Example.test:8443":     "forge.example.test:8443",
		"https://forge.example.test:8443/api": "forge.example.test:8443",
		"https://[::1]:8443":                  "[::1]:8443",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}

		if got, keyErr := credentialAuthorityKey(parsed); keyErr != nil || got != want {
			t.Errorf("credentialAuthorityKey(%q) = %q, %v, want %q", raw, got, keyErr, want)
		}
	}

	for _, raw := range []string{"http://forge.example.test", "https://user@forge.example.test", "https://user:secret@forge.example.test", "https:///path", "ftp://forge.example.test"} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}

		if _, keyErr := credentialAuthorityKey(parsed); !errors.Is(keyErr, errs.ErrValidation) {
			t.Errorf("credentialAuthorityKey(%q) = %v, want a validation refusal", raw, keyErr)
		}
	}

	if _, err := credentialAuthorityKey(nil); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("nil URL = %v, want a validation refusal", err)
	}
}

// recordingTransport answers in memory and counts what reached it.
type recordingTransport struct {
	requests []string
	respond  func(*http.Request) *http.Response
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request.URL.String())

	if transport.respond != nil {
		return transport.respond(request), nil
	}

	recorder := httptest.NewRecorder()
	recorder.WriteHeader(http.StatusNoContent)

	response := recorder.Result()
	response.Request = request

	return response, nil
}

func TestAuthorityTransport_DelegatesOnlyApprovedAuthorities(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://forge.example.test:8443", "https://registry.example.test"}

	for name, tc := range map[string]struct {
		url      string
		approved bool
	}{
		"approved explicit port":           {url: "https://forge.example.test:8443/api/v1/user", approved: true},
		"approved default port":            {url: "https://registry.example.test/v2/", approved: true},
		"approved default port spelled":    {url: "https://registry.example.test:443/v2/", approved: true},
		"approved host in another case":    {url: "https://FORGE.example.test:8443/", approved: true},
		"same host on the default port":    {url: "https://forge.example.test/api"},
		"same host on another port":        {url: "https://registry.example.test:8443/v2/"},
		"downgrade to http":                {url: "http://forge.example.test:8443/api"},
		"userinfo on an approved host":     {url: "https://user:secret@forge.example.test:8443/api"}, //nolint:gosec // Synthetic userinfo that must be refused.
		"subdomain of an approved host":    {url: "https://evil.forge.example.test:8443/"},
		"approved host as a suffix":        {url: "https://forge.example.test.evil.test:8443/"},
		"separately scoped fulcio origin":  {url: "https://fulcio.example.test/api/v2/signingCert"},
		"loopback not in the approved set": {url: "https://127.0.0.1:8443/"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport, err := newAuthorityTransport(allowed, nil)
			if err != nil {
				t.Fatal(err)
			}

			recorder := &recordingTransport{}
			transport.inner = recorder

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, nil)
			request.Header.Set("Authorization", "Bearer fixture-secret")

			response, err := transport.RoundTrip(request)
			if response != nil {
				_ = response.Body.Close()
			}

			if tc.approved {
				if err != nil || len(recorder.requests) != 1 {
					t.Fatalf("approved %s: err = %v, delegated %d", tc.url, err, len(recorder.requests))
				}

				return
			}

			if !errors.Is(err, errs.ErrValidation) || len(recorder.requests) != 0 {
				t.Fatalf("unapproved %s: err = %v, delegated %v", tc.url, err, recorder.requests)
			}

			if strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "secret@") {
				t.Fatalf("refusal echoes a credential: %v", err)
			}
		})
	}
}

func TestAuthorityTransport_RedirectsAreJudgedBeforeDelegation(t *testing.T) {
	t.Parallel()

	transport, err := newAuthorityTransport([]string{"https://forge.example.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := &recordingTransport{respond: func(request *http.Request) *http.Response {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Location", "https://collector.example.test/steal")
		recorder.WriteHeader(http.StatusTemporaryRedirect)

		response := recorder.Result()
		response.Request = request

		return response
	}}
	transport.inner = recorder

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://forge.example.test/api", nil)
	request.RequestURI = ""

	response, err := (&http.Client{Transport: transport}).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if !errors.Is(err, errs.ErrValidation) || !slices.Equal(recorder.requests, []string{"https://forge.example.test/api"}) {
		t.Fatalf("redirect: err = %v, delegated %v, want only the approved request", err, recorder.requests)
	}
}

func TestNewAuthorityTransport_ApprovesOnlyOrigins(t *testing.T) {
	t.Parallel()

	for _, authorities := range [][]string{
		nil,
		{},
		{"https://forge.example.test/api"},
		{"https://forge.example.test?x=1"},
		{"https://forge.example.test#x"},
		{"http://forge.example.test"},
		{"https://user@forge.example.test"},
		{"https://forge.example.test", "https://other.example.test/path"},
	} {
		if _, err := newAuthorityTransport(authorities, nil); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("newAuthorityTransport(%q) = %v, want a validation refusal", authorities, err)
		}
	}

	if _, err := newAuthorityTransport([]string{"https://forge.example.test/"}, []byte("not PEM")); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("unusable CA = %v, want a validation refusal", err)
	}
}

// TestCredentialAuthorities_ScopesAreExactAndDistinct holds each scope to the
// authorities its contract root projects, in order, and keeps the classes
// apart: another scope's authorities, an extra or missing root, or a reordered
// registry pair are refused.
func TestCredentialAuthorities_ScopesAreExactAndDistinct(t *testing.T) {
	t.Parallel()

	base := Target{
		Host:                "gitlab.compose.forgelab:8443",
		RegistryOrigin:      "https://registry.gitlab.compose.forgelab:8443",
		FulcioURL:           "https://fulcio.compose.forgelab:8443",
		ForgeAuthorities:    []string{"https://gitlab.compose.forgelab:8443"},
		RegistryAuthorities: []string{"https://registry.gitlab.compose.forgelab:8443", "https://gitlab.compose.forgelab:8443"},
		OIDCAuthorities:     []string{"https://fulcio.compose.forgelab:8443"},
	}

	for scope, want := range map[CredentialScope][]string{
		CredentialScopeForge:    base.ForgeAuthorities,
		CredentialScopeRegistry: base.RegistryAuthorities,
		CredentialScopeOIDC:     base.OIDCAuthorities,
	} {
		if got, err := credentialAuthorities(base, scope); err != nil || !slices.Equal(got, want) {
			t.Errorf("scope %d = %v, %v, want %v", scope, got, err, want)
		}
	}

	for name, tc := range map[string]struct {
		mutate func(*Target)
		scope  CredentialScope
	}{
		"forge scope given the registry set": {mutate: func(target *Target) { target.ForgeAuthorities = target.RegistryAuthorities }, scope: CredentialScopeForge},
		"forge scope with an extra root": {mutate: func(target *Target) {
			target.ForgeAuthorities = append(slices.Clone(target.ForgeAuthorities), target.FulcioURL)
		}, scope: CredentialScopeForge},
		"registry scope reordered":             {mutate: func(target *Target) { slices.Reverse(target.RegistryAuthorities) }, scope: CredentialScopeRegistry},
		"registry scope missing the forge":     {mutate: func(target *Target) { target.RegistryAuthorities = target.RegistryAuthorities[:1] }, scope: CredentialScopeRegistry},
		"registry scope given the OIDC set":    {mutate: func(target *Target) { target.RegistryAuthorities = target.OIDCAuthorities }, scope: CredentialScopeRegistry},
		"OIDC scope given the forge set":       {mutate: func(target *Target) { target.OIDCAuthorities = target.ForgeAuthorities }, scope: CredentialScopeOIDC},
		"OIDC scope without a Fulcio root":     {mutate: func(target *Target) { target.FulcioURL = "" }, scope: CredentialScopeOIDC},
		"forge scope empty":                    {mutate: func(target *Target) { target.ForgeAuthorities = nil }, scope: CredentialScopeForge},
		"unknown scope":                        {mutate: func(*Target) {}, scope: CredentialScope(9)},
		"forge root with userinfo in the host": {mutate: func(target *Target) { target.Host = "user@gitlab.compose.forgelab:8443" }, scope: CredentialScopeForge},
	} {
		target := base
		target.ForgeAuthorities = slices.Clone(base.ForgeAuthorities)
		target.RegistryAuthorities = slices.Clone(base.RegistryAuthorities)
		target.OIDCAuthorities = slices.Clone(base.OIDCAuthorities)
		tc.mutate(&target)

		if got, err := credentialAuthorities(target, tc.scope); !errors.Is(err, errs.ErrValidation) {
			t.Errorf("%s: %v, %v, want a validation refusal", name, got, err)
		}
	}
}

// TestCredentialProxy_RefusesBeforeDialing drives the proxy handler in memory:
// a non-CONNECT method, an unparsable or credential-bearing authority and an
// unapproved authority are answered without a tunnel or a dial.
func TestCredentialProxy_RefusesBeforeDialing(t *testing.T) {
	t.Parallel()

	proxy, err := newCredentialProxy([]string{"https://registry.example.test"}, func(_ context.Context, _, address string) (net.Conn, error) {
		t.Errorf("dialled %s for a refused request", address)

		return nil, net.ErrClosed
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		method, host string
		status       int
	}{
		"plain GET":                 {method: http.MethodGet, host: "registry.example.test:443", status: http.StatusMethodNotAllowed},
		"unapproved authority":      {method: http.MethodConnect, host: "collector.example.test:443", status: http.StatusForbidden},
		"approved host, other port": {method: http.MethodConnect, host: "registry.example.test:8443", status: http.StatusForbidden},
		"userinfo authority":        {method: http.MethodConnect, host: "user:secret@registry.example.test:443", status: http.StatusBadRequest},
		"empty authority":           {method: http.MethodConnect, host: "", status: http.StatusBadRequest},
	} {
		request := httptest.NewRequestWithContext(t.Context(), tc.method, "https://placeholder.invalid/", nil)
		request.Host = tc.host
		recorder := httptest.NewRecorder()

		proxy.ServeHTTP(recorder, request)

		if recorder.Code != tc.status {
			t.Errorf("%s: status %d, want %d (%s)", name, recorder.Code, tc.status, recorder.Body)
		}

		if strings.Contains(recorder.Body.String(), "secret") {
			t.Errorf("%s: refusal echoes the credential: %s", name, recorder.Body)
		}

		if len(proxy.open) != 0 {
			t.Errorf("%s: a tunnel was opened", name)
		}
	}
}
