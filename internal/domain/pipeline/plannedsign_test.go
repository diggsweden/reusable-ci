// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// TestPlanSign_PrecomputedFlags locks the semantic booleans the workflows read
// instead of comparing the method string, so a new method can't silently flip
// a workflow gate.
func TestPlanSign_PrecomputedFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		method          domainrelease.SignMethod
		idToken         bool
		importsGPG      bool
		signsContainers bool
	}{
		{"empty defaults to gpg", "", false, true, false},
		{"gpg", domainrelease.SignMethodGPG, false, true, false},
		{"sigstore", domainrelease.SignMethodSigstore, true, false, true},
		{"kms", domainrelease.SignMethodKMS, false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plan := pipeline.NewConfigPlan(&config.Config{Sign: config.SignConfig{Method: tc.method}})
			got := plan.Sign

			if got.RequiresIDToken != tc.idToken {
				t.Errorf("RequiresIDToken = %v, want %v", got.RequiresIDToken, tc.idToken)
			}

			if got.ImportsGPGKey != tc.importsGPG {
				t.Errorf("ImportsGPGKey = %v, want %v", got.ImportsGPGKey, tc.importsGPG)
			}

			if got.SignsContainers != tc.signsContainers {
				t.Errorf("SignsContainers = %v, want %v", got.SignsContainers, tc.signsContainers)
			}
		})
	}
}

// TestPlanSign_SigstoreEgressFollowsTheTransparencyChoice pins the egress
// flag the workflows use to decide whether a run may reach the public
// Sigstore services. kms with transparency none signs against the KMS alone
// and needs no egress; kms publishing to the log and keyless both do.
func TestPlanSign_SigstoreEgressFollowsTheTransparencyChoice(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		sign config.SignConfig
		want bool
	}{
		"kms none":    {sign: config.SignConfig{Method: domainrelease.SignMethodKMS, Transparency: domainrelease.TransparencyNone}, want: false},
		"kms public":  {sign: config.SignConfig{Method: domainrelease.SignMethodKMS, Transparency: domainrelease.TransparencyPublic}, want: true},
		"sigstore":    {sign: config.SignConfig{Method: domainrelease.SignMethodSigstore}, want: true},
		"gpg default": {sign: config.SignConfig{}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plan := pipeline.NewConfigPlan(&config.Config{Sign: testCase.sign})
			if plan.Sign.RequiresSigstoreEgress != testCase.want {
				t.Errorf("RequiresSigstoreEgress = %v, want %v (%+v)", plan.Sign.RequiresSigstoreEgress, testCase.want, plan.Sign)
			}
		})
	}
}
