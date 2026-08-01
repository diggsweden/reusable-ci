// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestProvenanceSignAllow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		keyRef string
		want   []string
	}{
		{"env_key_isolated", "env://COSIGN_KEY", []string{"COSIGN_KEY", "COSIGN_PASSWORD"}},
		{"env_key_custom_var", "env://MY_SIGNING_KEY", []string{"MY_SIGNING_KEY", "COSIGN_PASSWORD"}},
		{"kms_not_isolated", "hashivault://transit/keys/release", nil},
		{"file_not_isolated", "/keys/cosign.key", nil},
		{"empty_env_prefix_not_isolated", "env://", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := provenanceSignAllow(tc.keyRef); !slices.Equal(got, tc.want) {
				t.Errorf("provenanceSignAllow(%q) = %v, want %v", tc.keyRef, got, tc.want)
			}
		})
	}
}

func TestProvenanceSignBlobInput(t *testing.T) {
	t.Parallel()

	t.Run("sigstore_is_keyless", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "sigstore", oidcIssuer: "https://issuer"}, "p.json.bundle")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if !got.Keyless || got.KeyRef != "" || got.OIDCIssuer != "https://issuer" || got.BundlePath != "p.json.bundle" {
			t.Errorf("sigstore mapping wrong: %+v", got)
		}
	})

	t.Run("kms_uses_key", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms", keyRef: "hashivault://k"}, "b")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got.Keyless || got.KeyRef != "hashivault://k" {
			t.Errorf("kms mapping wrong: %+v", got)
		}
	})

	t.Run("bare_key_defaults_to_kms", func(t *testing.T) {
		t.Parallel()

		got, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", keyRef: "env://K"}, "b")
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}

		if got.KeyRef != "env://K" || got.Keyless {
			t.Errorf("bare-key mapping wrong: %+v", got)
		}
	})

	t.Run("kms_without_key_errors", func(t *testing.T) {
		t.Parallel()

		if _, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "kms"}, "b"); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})

	t.Run("gpg_rejected", func(t *testing.T) {
		t.Parallel()

		if _, err := provenanceSignBlobInput(signProvenanceInput{output: "p.json", method: "gpg"}, "b"); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err = %v, want ErrUsage", err)
		}
	})
}
