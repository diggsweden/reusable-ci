// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"fmt"
	"regexp"
)

// DevVersionDefaultBase is the BASE_VERSION when no semver tag exists.
const DevVersionDefaultBase = "0.0.0"

// DevShortSHALen is how many hex chars of the SHA appear in the dev-version
// suffix. Matches `git rev-parse --short=7`.
const DevShortSHALen = 7

// ComposeDevVersion produces the canonical dev-version tag:
//
//	{baseVersion}-dev-{branch-sanitised}-{shortSHA}
//
// Example: ComposeDevVersion("0.5.9", "feat/awesome", "abc1234")
//
//	→ "0.5.9-dev-feat-awesome-abc1234"
//
// branch is sanitised via SanitizePathToken; shortSHA is used verbatim
// (callers responsible for clamping to 7 chars).
func ComposeDevVersion(baseVersion, branch, shortSHA string) string {
	if baseVersion == "" {
		baseVersion = DevVersionDefaultBase
	}

	return fmt.Sprintf("%s-dev-%s-%s",
		baseVersion, SanitizePathToken(branch), shortSHA)
}

// semverTagPattern matches strict v-prefixed semver tags (no pre-release
// suffix). Used internally when picking the "latest stable" tag for the
// dev-version base. Matches glob `v[0-9]*.[0-9]*.[0-9]*`
// interpreted strictly.
//
// Unexported: the package-level public "is this a semver tag?" answer
// lives in domain/validate.SemverTagPattern (permissive, includes
// pre-release suffix). This pattern is the stricter dev-version-specific
// variant and is not part of any consumer's API.
var semverTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// StripVPrefix turns "v1.2.3" into "1.2.3". Idempotent on already-stripped
// inputs.
func StripVPrefix(tag string) string {
	if len(tag) > 0 && tag[0] == 'v' {
		return tag[1:]
	}

	return tag
}

// LatestSemverTag returns the highest-versioned tag that strictly matches
// semverTagPattern, comparing lexically by their numeric components.
// Returns "" when the input is empty or no tag matches.
//
// Pure: callers gather the candidate tags (e.g. via `git tag -l`).
// adapter/git wraps that side and feeds the result here.
func LatestSemverTag(tags []string) string {
	var (
		best      string
		bestParts [3]int
	)

	for _, t := range tags { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if !semverTagPattern.MatchString(t) {
			continue
		}

		parts, ok := parseSemverParts(StripVPrefix(t))
		if !ok {
			continue
		}

		if best == "" || compareSemver(parts, bestParts) > 0 {
			best = t
			bestParts = parts
		}
	}

	return best
}

// parseSemverParts splits "1.2.3" into [1,2,3].
func parseSemverParts(v string) ([3]int, bool) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	var (
		parts [3]int
		cur   int
		idx   int
	)

	for i := range len(v) {
		c := v[i] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch {
		case c == '.':
			if idx == 2 {
				return parts, false
			}

			parts[idx] = cur
			idx++
			cur = 0
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
		default:
			return parts, false
		}
	}

	if idx != 2 {
		return parts, false
	}

	parts[2] = cur

	return parts, true
}

func compareSemver(a, b [3]int) int {
	for i := range 3 {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return 1
			}

			return -1
		}
	}

	return 0
}
