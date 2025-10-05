// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
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

// Semver is a validated, strict three-component semantic version. Version has
// no leading v; Prerelease and Build have no leading separator.
type Semver struct {
	Version    string
	Major      string
	Minor      string
	Patch      string
	Prerelease string
	Build      string
}

// ParseSemver validates a strict MAJOR.MINOR.PATCH semantic version through
// x/mod/semver. A leading lowercase v is optional; abbreviated x/mod forms such
// as v1 and v1.2 are deliberately rejected.
func ParseSemver(value string) (Semver, bool) {
	normalized := value
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}

	if !semver.IsValid(normalized) {
		return Semver{}, false
	}

	version := strings.TrimPrefix(normalized, "v")

	core := version
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, false
	}

	return Semver{
		Version:    version,
		Major:      parts[0],
		Minor:      parts[1],
		Patch:      parts[2],
		Prerelease: strings.TrimPrefix(semver.Prerelease(normalized), "-"),
		Build:      strings.TrimPrefix(semver.Build(normalized), "+"),
	}, true
}

// ParseSemverTag is ParseSemver with the repository's lowercase-v tag
// convention enforced.
func ParseSemverTag(tag string) (Semver, bool) {
	if !strings.HasPrefix(tag, "v") {
		return Semver{}, false
	}

	return ParseSemver(tag)
}

// IsStableSemverTag reports whether tag strictly matches vMAJOR.MINOR.PATCH.
// It deliberately rejects prerelease/build metadata; release signing paths use
// this narrower predicate so a request like v1.2.3-rc1 cannot reach signing.
func IsStableSemverTag(tag string) bool {
	parsed, ok := ParseSemverTag(tag)

	return ok && parsed.Prerelease == "" && parsed.Build == ""
}

// StripVPrefix turns "v1.2.3" into "1.2.3". Idempotent on already-stripped
// inputs.
func StripVPrefix(tag string) string {
	if len(tag) > 0 && tag[0] == 'v' {
		return tag[1:]
	}

	return tag
}

// LatestSemverTag returns the highest stable v-prefixed semantic-version tag.
// Returns "" when the input is empty or no tag matches.
//
// Pure: callers gather the candidate tags (e.g. via `git tag -l`).
// adapter/git wraps that side and feeds the result here.
func LatestSemverTag(tags []string) string {
	var best string

	for _, t := range tags { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if !IsStableSemverTag(t) {
			continue
		}

		if best == "" || semver.Compare(t, best) > 0 {
			best = t
		}
	}

	return best
}
