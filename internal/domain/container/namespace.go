// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// DefaultRegistry is the registry the namespace and auth policies assume
// when none is configured (GitHub Container Registry). Single source for
// that default across the container namespace check, the publish auth
// fallback, and the registry-validation flag. Adopters on a different
// registry override it at the call site (ENFORCE_NAMESPACE_ON / the
// expected-registry flag), not here.
const DefaultRegistry = "ghcr.io"

// ValidateNamespaceInput is the input for ValidateNamespace.
type ValidateNamespaceInput struct {
	ImageName        string // resolved image (e.g. "<registry>/owner/repo[-suffix|/sub]")
	Repository       string // "owner/repo"
	Registry         string // the image's registry, e.g. "ghcr.io"
	EnforceNamespace string // expected owner segment under the registry

	// EnforceOnRegistries lists the registries whose namespace policy this
	// deployment owns and therefore enforces. An image on any other registry
	// is skipped (returns nil) — those registries run their own policy. Empty
	// defaults to {DefaultRegistry}, preserving the historical ghcr.io-only
	// behaviour; an adopter on a different registry sets this to theirs so the
	// check actually runs instead of silently passing.
	EnforceOnRegistries []string
}

// ValidateNamespace enforces the namespace policy for the configured
// registry: an image must live at <registry>/<EnforceNamespace>/<repo-short>,
// optionally with a single "-suffix" segment OR a "/subpath" — but not both,
// since the combined shape (e.g. "<registry>/org/repo-evil/payload") would let
// a sibling top-level package masquerade as a subpath.
//
// Registries outside EnforceOnRegistries are skipped (returns nil) — they run
// their own policy. The enforced registry is taken from in.Registry, so the
// same check works for ghcr.io (the default) or any self-hosted registry an
// adopter configures.
func ValidateNamespace(in ValidateNamespaceInput) error {
	enforceOn := in.EnforceOnRegistries
	if len(enforceOn) == 0 {
		enforceOn = []string{DefaultRegistry}
	}

	if !slices.Contains(enforceOn, in.Registry) {
		return nil
	}

	repoShort := repoShortName(in.Repository)
	expectedPrefix := fmt.Sprintf("%s/%s/%s", in.Registry, in.EnforceNamespace, repoShort)
	pattern := "^" + regexp.QuoteMeta(expectedPrefix) + `(-[^/]*|/.*)?$`

	matched, err := regexp.MatchString(pattern, in.ImageName)
	if err != nil {
		return fmt.Errorf("validate namespace: build regex: %w", err)
	}

	if !matched {
		return &NamespaceViolationError{
			ImageName:      in.ImageName,
			ExpectedPrefix: expectedPrefix,
		}
	}

	return nil
}

// NamespaceViolationError is returned when an image lands outside the allowed
// namespace for its registry.
type NamespaceViolationError struct {
	ImageName      string
	ExpectedPrefix string
}

func (e *NamespaceViolationError) Error() string {
	return fmt.Sprintf(
		"image %q is outside the allowed namespace; allowed: %q, %q-<suffix>, or %q/<subpath>",
		e.ImageName, e.ExpectedPrefix, e.ExpectedPrefix, e.ExpectedPrefix,
	)
}

// Unwrap ties a namespace violation to ErrValidation so main()'s exit-code
// ladder maps it to EX_VALIDATION (1) — a domain-rule failure ("namespace
// forbidden", per the errs taxonomy) — rather than the unclassified
// EX_SOFTWARE (70) that signals an internal bug.
func (e *NamespaceViolationError) Unwrap() error { return errs.ErrValidation }

// Compile-time check.
var _ error = (*NamespaceViolationError)(nil)
