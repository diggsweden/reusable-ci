// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package publish hosts pure decision tables for the publish-side
// pre-flights (registry auth combinations, future credential gating).
// Adapters and I/O live in internal/app/publish.
package publish

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// RegistryAuthInput describes the auth configuration the workflow wants
// to use. Mirrors the four positional / env inputs of.
type RegistryAuthInput struct {
	UseCIToken       bool
	Registry         string
	ExpectedRegistry string // empty → "ghcr.io"
	HasPassword      bool
}

// RegistryAuthResult lists fatal errors and advisory warnings produced
// by ValidateRegistryAuth. The "OK" path is Errors == nil.
type RegistryAuthResult struct {
	Errors   []string
	Warnings []string
}

// ValidateRegistryAuth applies the bash's two checks:
//
//   - Error when use-ci-token=false and no registry-password is set
//     (custom auth without credentials would fail at upload time).
//   - Warning when use-ci-token=true and Registry differs from
//     ExpectedRegistry (the CI token only authenticates against the
//     platform-default registry).
//
// Pure: takes inputs, returns a result. Caller is responsible for
// printing and exit-coding.
func ValidateRegistryAuth(in RegistryAuthInput) RegistryAuthResult {
	expected := in.ExpectedRegistry
	if expected == "" {
		expected = container.DefaultRegistry
	}

	var res RegistryAuthResult
	if !in.UseCIToken && !in.HasPassword {
		res.Errors = append(res.Errors, "registry-password secret is required when use-ci-token=false")
	}

	if in.Registry != expected && in.UseCIToken {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("Using CI token with non-%s registry (%s)", expected, in.Registry))
		res.Warnings = append(res.Warnings,
			"This will likely fail. Set use-ci-token=false and provide registry-password secret")
	}

	return res
}
