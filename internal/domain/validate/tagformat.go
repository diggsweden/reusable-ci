// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package validate is the pure-domain home for ref / tag / changelog
// validators driven by `reusable-ci validate ...` subcommands. Network /
// file / git access lives in adapters and use cases.
package validate

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
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

	parsed, ok := domainversion.ParseSemverTag(tag)
	if !ok {
		return nil, fmt.Errorf("invalid tag format: %q: %w", tag, errs.ErrValidation)
	}

	tf := &TagFormat{
		Tag:        tag,
		Major:      parsed.Major,
		Minor:      parsed.Minor,
		Patch:      parsed.Patch,
		Prerelease: parsed.Prerelease,
		Build:      parsed.Build,
	}
	if tf.Prerelease == "" {
		tf.PrereleaseStandard = true
	} else {
		tf.PrereleaseStandard = release.IsCanonicalPrerelease(tf.Prerelease)
	}

	return tf, nil
}
