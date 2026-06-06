// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
//nolint:gochecknoglobals // canonical prerelease identifier list.
var PrereleaseIdentifiers = []string{"alpha", "beta", "rc", "dev", "snapshot", "SNAPSHOT"}

// prereleasePattern matches the canonical pre-release identifiers
// embedded in a tag name (with the leading dash).
//
//nolint:gochecknoglobals // precompiled regex derived from the list above.
var prereleasePattern = regexp.MustCompile(`-(` + strings.Join(PrereleaseIdentifiers, "|") + `)`)

// IsPrereleaseTag reports whether the tag name embeds a known
// pre-release identifier (alpha/beta/rc/dev/snapshot/SNAPSHOT).
func IsPrereleaseTag(tag string) bool {
	return prereleasePattern.MatchString(tag)
}
