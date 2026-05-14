// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"errors"
	"fmt"
	"regexp"
)

// ValidateNamespaceInput is the input for ValidateNamespace.
type ValidateNamespaceInput struct {
	ImageName        string // resolved image (e.g. "ghcr.io/owner/repo[-suffix|/sub]")
	Repository       string // "owner/repo"
	Registry         string // "ghcr.io" / other
	EnforceNamespace string // expected owner segment under the registry
}

// ValidateNamespace enforces the ghcr.io namespace policy: an image must
// live at ghcr.io/<EnforceNamespace>/<repo-short>, optionally with a single
// "-suffix" segment OR a "/subpath" — but not both, since the combined
// shape (e.g. "ghcr.io/org/repo-evil/payload") would let a sibling top-level
// package masquerade as a subpath.
//
// Non-ghcr.io registries are skipped (returns nil) — this is deliberately
// registry-specific; other registries enforce their own policies.
//
// Mirrors scripts/container/validate-namespace.sh exactly.
func ValidateNamespace(in ValidateNamespaceInput) error {
	if in.Registry != "ghcr.io" {
		return nil
	}

	repoShort := repoShortName(in.Repository)
	expectedPrefix := fmt.Sprintf("ghcr.io/%s/%s", in.EnforceNamespace, repoShort)
	pattern := "^" + regexp.QuoteMeta(expectedPrefix) + `(-[^/]*|/.*)?$`

	matched, err := regexp.MatchString(pattern, in.ImageName)
	if err != nil {
		return fmt.Errorf("validate namespace: build regex: %w", err)
	}
	if !matched {
		return &NamespaceViolation{
			ImageName:      in.ImageName,
			ExpectedPrefix: expectedPrefix,
		}
	}
	return nil
}

// NamespaceViolation is returned when an image lands outside the allowed
// ghcr.io namespace.
type NamespaceViolation struct {
	ImageName      string
	ExpectedPrefix string
}

func (e *NamespaceViolation) Error() string {
	return fmt.Sprintf(
		"image %q is outside the allowed namespace; allowed: %q, %q-<suffix>, or %q/<subpath>",
		e.ImageName, e.ExpectedPrefix, e.ExpectedPrefix, e.ExpectedPrefix,
	)
}

// IsNamespaceViolation reports whether err (or anything it wraps) is a
// NamespaceViolation. Convenience wrapper for callers that need to branch.
func IsNamespaceViolation(err error) bool {
	if err == nil {
		return false
	}
	// Avoid importing errors for one Is check; do the unwrap by hand to keep
	// the package's import surface minimal.
	for err != nil {
		namespaceViolation := &NamespaceViolation{}
		if errors.As(err, &namespaceViolation) {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Compile-time check.
var _ error = (*NamespaceViolation)(nil)
