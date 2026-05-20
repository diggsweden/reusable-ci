// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestGitSigningConfig_EmptyDefaultsToGPG(t *testing.T) {
	t.Parallel()

	if got := (config.GitSigningConfig{}).EffectiveMethod(); got != config.GitSignGPG {
		t.Errorf("EffectiveMethod() = %q, want gpg", got)
	}
}

func TestGitSigningConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  config.GitSignMethod
		wantErr bool
	}{
		{"empty ok (gpg)", "", false},
		{"gpg ok", config.GitSignGPG, false},
		{"ssh ok", config.GitSignSSH, false},
		{"gitsign rejected", "gitsign", true},
		{"garbage rejected", "openssl", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := config.GitSigningConfig{Method: tc.method}.Validate()
			if tc.wantErr {
				if !errors.Is(err, errs.ErrInvalidConfig) {
					t.Errorf("method %q: want ErrInvalidConfig, got %v", tc.method, err)
				}

				return
			}

			if err != nil {
				t.Errorf("method %q: unexpected error %v", tc.method, err)
			}
		})
	}
}
