// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"regexp"
	"strings"
)

// PrereleaseIdentifiers is the canonical list of pre-release tokens
// recognised across the project. Single source of truth — both the
// "tag carries a prerelease marker?" check here and the "the suffix
// after `-` matches a known token?" check in domain/validate build
// their regexes from this list.
//
// Mirrors CI_PRERELEASE_IDENTIFIERS in scripts/ci/output.sh.
var PrereleaseIdentifiers = []string{"alpha", "beta", "rc", "dev", "snapshot", "SNAPSHOT"}

// prereleasePattern matches the canonical pre-release identifiers
// embedded in a tag name (with the leading dash).
var prereleasePattern = regexp.MustCompile(`-(` + strings.Join(PrereleaseIdentifiers, "|") + `)`)

// IsPrereleaseTag reports whether the tag name embeds a known
// pre-release identifier (alpha/beta/rc/dev/snapshot/SNAPSHOT).
func IsPrereleaseTag(tag string) bool {
	return prereleasePattern.MatchString(tag)
}
