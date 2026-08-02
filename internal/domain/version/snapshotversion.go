// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"fmt"
	"regexp"
)

// SnapshotVersionDefaultBase is the BASE_VERSION when no semver tag exists.
const SnapshotVersionDefaultBase = "0.0.0"

// SnapshotShortSHALen is how many hex chars of the SHA appear in the snapshot-version
// suffix. Matches `git rev-parse --short=7`.
const SnapshotShortSHALen = 7

// ComposeSnapshotVersion produces the canonical snapshot-version tag:
//
//	{baseVersion}-snapshot-{branch-sanitised}-{shortSHA}
//
// Example: ComposeSnapshotVersion("0.5.9", "feat/awesome", "abc1234")
//
//	→ "0.5.9-snapshot-feat-awesome-abc1234"
//
// branch is sanitised via SanitizePathToken; shortSHA is used verbatim
// (callers responsible for clamping to 7 chars).
func ComposeSnapshotVersion(baseVersion, branch, shortSHA string) string {
	if baseVersion == "" {
		baseVersion = SnapshotVersionDefaultBase
	}

	return fmt.Sprintf("%s-snapshot-%s-%s",
		baseVersion, SanitizePathToken(branch), shortSHA)
}

// semverTagPattern matches strict v-prefixed semver tags (no pre-release
// suffix). Used internally when picking the "latest stable" tag for the
// snapshot-version base. Matches glob `v[0-9]*.[0-9]*.[0-9]*`
// interpreted strictly.
//
// Unexported: the package-level public "is this a semver tag?" answer
// lives in domain/validate.SemverTagPattern (permissive, includes
// pre-release suffix). This pattern is the stricter snapshot-version-specific
// variant and is not part of any consumer's API.
var semverTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// IsStableSemverTag reports whether tag strictly matches vMAJOR.MINOR.PATCH.
// It deliberately rejects prerelease/build metadata; release signing paths use
// this narrower predicate so a request like v1.2.3-rc1 cannot reach signing.
func IsStableSemverTag(tag string) bool {
	return semverTagPattern.MatchString(tag)
}

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
