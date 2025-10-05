// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func TestSignConfig_EmptyDefaultsToGPG(t *testing.T) {
	t.Parallel()

	if got := (config.SignConfig{}).EffectiveMethod(); got != domainrelease.SignMethodGPG {
		t.Errorf("empty sign block must default to gpg, got %q", got)
	}
}

func TestSignConfig_Validate_HappyPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   config.SignConfig
	}{
		{"empty", config.SignConfig{}},
		{"gpg explicit", config.SignConfig{Method: domainrelease.SignMethodGPG}},
		{"sigstore minimal", config.SignConfig{Method: domainrelease.SignMethodSigstore}},
		{"sigstore with issuer", config.SignConfig{Method: domainrelease.SignMethodSigstore, OIDCIssuer: "https://gitlab.diggsweden.internal"}},
		{"kms hashivault", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "hashivault://transit/keys/release"}},
		{"kms awskms", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "awskms:///alias/release-signing"}},
		{"kms gcpkms", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "gcpkms://projects/p/locations/l/keyRings/r/cryptoKeys/k"}},
		{"kms azurekms", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "azurekms://vault.azure.net/keys/k/v"}},
		{"kms pkcs11", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "pkcs11:object=hsm-key"}},
		{"kms file (local dev)", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "file:./signing.key"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if err := c.in.Validate(); err != nil {
				t.Errorf("%+v: unexpected error: %v", c.in, err)
			}
		})
	}
}

func TestSignConfig_Validate_RejectsBadFieldCombinations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   config.SignConfig
	}{
		{"gpg with key", config.SignConfig{Method: domainrelease.SignMethodGPG, Key: "awskms:///alias/X"}},
		{"gpg with issuer", config.SignConfig{Method: domainrelease.SignMethodGPG, OIDCIssuer: "https://x"}},
		{"sigstore with key", config.SignConfig{Method: domainrelease.SignMethodSigstore, Key: "awskms:///alias/X"}},
		{"kms missing key", config.SignConfig{Method: domainrelease.SignMethodKMS}},
		{"kms with issuer", config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "awskms:///alias/X", OIDCIssuer: "https://x"}},
		{"unknown method", config.SignConfig{Method: "openssl"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := c.in.Validate()
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("%+v: expected ErrInvalidConfig, got %v", c.in, err)
			}
		})
	}
}

func TestSignConfig_Validate_KMSKeySchemeAllowlist(t *testing.T) {
	t.Parallel()

	// Path traversal / arbitrary file: an attacker who can land a PR
	// must not be able to redirect signing to /etc/secrets or similar.
	// All these must fail.
	badKeys := []string{
		"/etc/secrets/private.key",
		"../../../tmp/private.key",
		"./local.key",         // no `file:` prefix → reject
		"http://attacker/key", // wrong scheme entirely
		"ftp://x",
		"javascript:alert(0)",
		"",

		// Near-misses on an allowed scheme. The comparison is exact
		// against the text before the first colon, and these are the
		// inputs that would slip through if it ever became a prefix or
		// substring match.
		"awskmsevil://alias/release",
		"evilawskms://alias/release",
		"awskms.attacker.example://alias/release",
		"AWSKMS://alias/release", // case-sensitive by design
		"://no-scheme",
	}

	for _, k := range badKeys {
		t.Run("reject "+k, func(t *testing.T) {
			t.Parallel()

			cfg := config.SignConfig{Method: domainrelease.SignMethodKMS, Key: k}

			err := cfg.Validate()
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("key %q must be rejected as ErrInvalidConfig, got %v", k, err)
			}
		})
	}
}

func TestSignConfig_Validate_OIDCIssuerMustHaveHTTPSAuthority(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{"http rejected", "http://example.com"},
		{"missing scheme", "example.com"},
		{"missing host", "https://"},
		{"javascript", "javascript:alert(0)"},
		{"port only", "https://:443"},
		{"empty IPv6 host", "https://[]:443"},
		{"unbracketed IPv6", "https://2001:db8::1"},
		{"bracketed non-IP", "https://[issuer.example]:443"},
		{"bracketed IPv4", "https://[192.0.2.1]:443"},
		{"extra colon", "https://issuer.example:443:443"},
		{"empty port", "https://issuer.example:/fixture-secret"},
		{"zero port", "https://issuer.example:0/fixture-secret"},
		{"port overflow", "https://issuer.example:65536/fixture-secret"},
		{"invalid port with secret", "https://issuer.example:fixture-secret"},
		{"userinfo", "https://user:fixture-secret@issuer.example"},
		{"missing host with secret", "https://:443/fixture-secret"},
		{"invalid scheme with secret", "http://issuer.example/fixture-secret"},
		{"invalid escape with secret", "https://issuer.example/%zz?token=fixture-secret"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.SignConfig{Method: domainrelease.SignMethodSigstore, OIDCIssuer: c.in}

			err := cfg.Validate()
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), "sign.oidc-issuer")
			require.NotContains(t, err.Error(), "fixture-secret")

			err = config.Validate(&config.Config{
				Artifacts: []config.Artifact{{Name: "app", ProjectType: "maven"}},
				Sign:      cfg,
			})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.Contains(t, err.Error(), "sign.oidc-issuer")
			require.NotContains(t, err.Error(), "fixture-secret")
		})
	}
}

func TestSignConfig_Validate_AcceptsOperatorSelectedHTTPSIssuers(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{
		"https://issuer.example",
		"https://Issuer.Example.:8443/realms/release",
		"https://localhost:443",
		"https://192.0.2.1:65535/oidc",
		"https://[2001:db8::1]/oidc",
		"https://[2001:db8::1]:8443/oidc",
	} {
		t.Run(issuer, func(t *testing.T) {
			t.Parallel()

			cfg := config.SignConfig{Method: domainrelease.SignMethodSigstore, OIDCIssuer: issuer}
			require.NoError(t, cfg.Validate())
			require.NoError(t, config.Validate(&config.Config{
				Artifacts: []config.Artifact{{Name: "app", ProjectType: "maven"}},
				Sign:      cfg,
			}))
		})
	}
}

func TestSignConfig_Validate_KMSKeyErrorMessageIsActionable(t *testing.T) {
	t.Parallel()

	cfg := config.SignConfig{Method: domainrelease.SignMethodKMS, Key: "./somekey"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}

	// The error must teach the operator how to fix the problem.
	for _, want := range []string{"hashivault", "awskms", "file:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %q for operator guidance; got:\n%s", want, err.Error())
		}
	}
}

// TestSignConfig_EffectiveTransparencyHonoursAnExplicitChoice covers the
// helper that decides whether a signature is published to the transparency log.
//
// Nothing referenced EffectiveTransparency, so a version returning the package
// default unconditionally passed every test here. That is not a cosmetic gap in
// either direction. `transparency: none` is an operator saying this signature
// must not be entered in a public log — a policy choice for a private or
// pre-release artifact — and silently publishing it anyway cannot be undone.
// Losing the default in the other direction silently stops logging signatures a
// verifier is expected to find.
func TestSignConfig_EffectiveTransparencyHonoursAnExplicitChoice(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   domainrelease.Transparency
		want domainrelease.Transparency
	}{
		{name: "omitted falls back to the default", in: "", want: domainrelease.DefaultTransparency},
		{name: "explicit public", in: domainrelease.TransparencyPublic, want: domainrelease.TransparencyPublic},
		{name: "explicit none", in: domainrelease.TransparencyNone, want: domainrelease.TransparencyNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := config.SignConfig{Transparency: tc.in}.EffectiveTransparency()
			if got != tc.want {
				t.Errorf("EffectiveTransparency() = %q, want %q", got, tc.want)
			}
		})
	}

	// The explicit values must not both collapse onto the default, which is
	// what a helper ignoring its input would do.
	public := config.SignConfig{Transparency: domainrelease.TransparencyPublic}.EffectiveTransparency()
	none := config.SignConfig{Transparency: domainrelease.TransparencyNone}.EffectiveTransparency()

	if public == none {
		t.Errorf("public and none both resolved to %q; the explicit choice is being ignored", public)
	}
}

// TestSignConfig_TransparencyAcrossMethods pins which methods accept which
// values, since the field means something different for each.
func TestSignConfig_TransparencyAcrossMethods(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		cfg       config.SignConfig
		wantError bool
		why       string
	}{
		{
			name: "gpg with no transparency is fine",
			cfg:  config.SignConfig{Method: domainrelease.SignMethodGPG},
		},
		{
			name:      "gpg forbids transparency outright",
			cfg:       config.SignConfig{Method: domainrelease.SignMethodGPG, Transparency: domainrelease.TransparencyPublic},
			wantError: true,
			why:       "OpenPGP has no transparency log, so the field would be silently meaningless",
		},
		{
			name:      "sigstore forbids none",
			cfg:       config.SignConfig{Method: domainrelease.SignMethodSigstore, Transparency: domainrelease.TransparencyNone},
			wantError: true,
			why:       "a keyless signature needs a Rekor inclusion proof to be verifiable at all",
		},
		{
			name: "sigstore accepts public",
			cfg:  config.SignConfig{Method: domainrelease.SignMethodSigstore, Transparency: domainrelease.TransparencyPublic},
		},
		{
			name:      "an unknown transparency value is refused",
			cfg:       config.SignConfig{Method: domainrelease.SignMethodSigstore, Transparency: domainrelease.Transparency("maybe")},
			wantError: true,
			why:       "an unrecognised value must not fall through to the default",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.Validate()
			if gotErr := err != nil; gotErr != tc.wantError {
				t.Fatalf("Validate() err = %v, wantError = %v: %s", err, tc.wantError, tc.why)
			}

			if tc.wantError && !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
