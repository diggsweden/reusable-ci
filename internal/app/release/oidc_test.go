// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// stubDescriber is a minimal provider.Describer for delegation tests.
// The concrete per-forge issuer values are tested in each adapter's
// describe_test.go; here we only assert DefaultOIDCIssuer's behaviour.
type stubDescriber struct{ info provider.Info }

func (s stubDescriber) Describe() provider.Info { return s.info }

func TestDefaultOIDCIssuer_DelegatesToDescriber(t *testing.T) {
	t.Parallel()

	got := apprelease.DefaultOIDCIssuer(stubDescriber{provider.Info{OIDCIssuer: "https://issuer.example"}})
	if got != "https://issuer.example" {
		t.Errorf("OIDC issuer = %q, want https://issuer.example", got)
	}
}

func TestDefaultOIDCIssuer_EmptyWhenProviderHasNoIssuer(t *testing.T) {
	t.Parallel()

	if got := apprelease.DefaultOIDCIssuer(stubDescriber{provider.Info{}}); got != "" {
		t.Errorf("OIDC issuer = %q, want empty", got)
	}
}

func TestDefaultOIDCIssuer_NilDescriberIsEmpty(t *testing.T) {
	t.Parallel()

	if got := apprelease.DefaultOIDCIssuer(nil); got != "" {
		t.Errorf("OIDC issuer = %q, want empty for nil describer", got)
	}
}

func TestKeylessNeedsIssuer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		explicitIssuer string
		keylessCapable bool
		want           bool
	}{
		{"forge_keyless_no_explicit", "", true, false},                   // GitHub: forge publishes an issuer
		{"forge_not_keyless_no_explicit", "", false, true},               // Forgejo: warn
		{"forge_not_keyless_explicit_issuer", "https://x", false, false}, // operator supplied → trust them
		{"forge_keyless_explicit_issuer", "https://x", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := apprelease.KeylessNeedsIssuer(tc.explicitIssuer, tc.keylessCapable); got != tc.want {
				t.Errorf("KeylessNeedsIssuer(%q, %v) = %v, want %v", tc.explicitIssuer, tc.keylessCapable, got, tc.want)
			}
		})
	}
}
