// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func TestSignConfig_EmptyDefaultsToGPG(t *testing.T) {
	if got := (config.SignConfig{}).EffectiveMethod(); got != domainrelease.SignMethodGPG {
		t.Errorf("empty sign block must default to gpg, got %q", got)
	}
}

func TestSignConfig_Validate_HappyPaths(t *testing.T) {
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
			if err := c.in.Validate(); err != nil {
				t.Errorf("%+v: unexpected error: %v", c.in, err)
			}
		})
	}
}

func TestSignConfig_Validate_RejectsBadFieldCombinations(t *testing.T) {
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
			err := c.in.Validate()
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("%+v: expected ErrInvalidConfig, got %v", c.in, err)
			}
		})
	}
}

func TestSignConfig_Validate_KMSKeySchemeAllowlist(t *testing.T) {
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
	}

	for _, k := range badKeys {
		t.Run("reject "+k, func(t *testing.T) {
			cfg := config.SignConfig{Method: domainrelease.SignMethodKMS, Key: k}

			err := cfg.Validate()
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("key %q must be rejected as ErrInvalidConfig, got %v", k, err)
			}
		})
	}
}

func TestSignConfig_Validate_OIDCIssuerMustBeHTTPS(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"http rejected", "http://example.com"},
		{"missing scheme", "example.com"},
		{"missing host", "https://"},
		{"javascript", "javascript:alert(0)"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.SignConfig{Method: domainrelease.SignMethodSigstore, OIDCIssuer: c.in}

			err := cfg.Validate()
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("oidc-issuer %q must be rejected as ErrInvalidConfig, got %v", c.in, err)
			}
		})
	}
}

func TestSignConfig_Validate_KMSKeyErrorMessageIsActionable(t *testing.T) {
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
