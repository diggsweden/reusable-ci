// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package validate is the pure-domain home for ref / tag / changelog
// validators driven by `reusable-ci validate ...` subcommands. Network /
// file / git access lives in adapters and use cases.
package validate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

// SemverTagPattern is the official SemVer 2.0.0 validation regex
// published by semver.org under "Is there a suggested regular
// expression (RegEx) to check a SemVer string?":
//
//	https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
//
// This is the numbered-capture form (semver.org also lists a
// named-capture variant), with a `v` prefix prepended for this
// project's tag convention. Body is verbatim from the spec page.
// Capture groups, in order:
//
//	[1] major
//	[2] minor
//	[3] patch
//	[4] pre-release (without the leading '-'), empty for stable releases
//	[5] build metadata (without the leading '+'), empty when absent
//
// Stricter than the bash heritage (CI_SEMVER_TAG_REGEX) in two ways
// worth catching at the CI gate: leading zeros are rejected (`v01.0.0`
// is not a valid SemVer), and the pre-release grammar enforces
// dot-separated identifiers with no leading-zero numeric segments.
var SemverTagPattern = regexp.MustCompile(
	`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`,
)

// PrereleaseSuffixPattern matches the project's standard pre-release
// identifiers (release.PrereleaseIdentifiers — the single source of
// truth) optionally followed by `.<n>`. Anything that doesn't match
// here is treated as informational only — it doesn't fail validation.
var PrereleaseSuffixPattern = regexp.MustCompile(
	`^(` + strings.Join(release.PrereleaseIdentifiers, "|") + `)(\.[0-9]+)?$`,
)

// TagFormat is the parsed form of a valid release tag.
type TagFormat struct {
	Tag        string
	Major      string
	Minor      string
	Patch      string
	Prerelease string // empty for stable releases
	Build      string // SemVer build metadata (after '+'); empty when absent

	// PrereleaseStandard reports whether Prerelease matches the project's
	// canonical identifier set. Non-standard identifiers are warnings
	// only — see ParseTagFormat semantics.
	PrereleaseStandard bool
}

// IsStable reports whether this is a stable release (no pre-release suffix).
func (t TagFormat) IsStable() bool { return t.Prerelease == "" }

// ParseTagFormat validates and parses the tag string. Returns nil
// TagFormat and an error when the tag doesn't match the canonical
// pattern. Non-standard pre-release identifiers do *not* error — they
// surface as PrereleaseStandard=false on the returned TagFormat.
func ParseTagFormat(tag string) (*TagFormat, error) {
	if tag == "" {
		return nil, fmt.Errorf("usage: validate tag-format <tag-name>: %w", errs.ErrUsage)
	}

	m := SemverTagPattern.FindStringSubmatch(tag) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if m == nil {
		return nil, fmt.Errorf("invalid tag format: %q: %w", tag, errs.ErrValidation)
	}

	tf := &TagFormat{
		Tag:        tag,
		Major:      m[1],
		Minor:      m[2],
		Patch:      m[3],
		Prerelease: m[4],
		Build:      m[5],
	}
	if tf.Prerelease == "" {
		tf.PrereleaseStandard = true
	} else {
		tf.PrereleaseStandard = PrereleaseSuffixPattern.MatchString(tf.Prerelease)
	}

	return tf, nil
}
