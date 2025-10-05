// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// PrereleaseIdentifiers is the canonical list of pre-release tokens
// recognised across the project. Single source of truth — both the
// "tag carries a prerelease marker?" check here and the canonical-suffix
// classification in domain/validate use this list.
//
//nolint:gochecknoglobals // canonical prerelease identifier list.
var PrereleaseIdentifiers = []string{"alpha", "beta", "rc", "dev", "snapshot", "SNAPSHOT"}

// IsCanonicalPrerelease reports whether a validated SemVer prerelease is one
// canonical identifier, optionally followed by one numeric component.
func IsCanonicalPrerelease(prerelease string) bool {
	identifier, sequence, hasSequence := strings.Cut(prerelease, ".")
	if !slices.Contains(PrereleaseIdentifiers, identifier) {
		return false
	}

	if !hasSequence {
		return true
	}

	if sequence == "" || strings.Contains(sequence, ".") {
		return false
	}

	for _, char := range sequence {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

// IsPrereleaseTag reports whether a valid v-prefixed semantic-version tag has
// any pre-release identifier. Canonical naming is a separate warning-level
// policy; a valid noncanonical suffix must still be published as a prerelease.
func IsPrereleaseTag(tag string) bool {
	parsed, ok := version.ParseSemverTag(tag)

	return ok && parsed.Prerelease != ""
}
