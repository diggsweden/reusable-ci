// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package selfrepo answers "which repository is this binary from?".
//
// It exists because reusable-ci runs inside OTHER repositories. Anything
// that needs to name reusable-ci itself — resolving a reusable-ci ref,
// checking that workflows pin reusable-ci to a tag — cannot use the run
// context, which describes the repository being built. On a forge there
// is no reliable ambient answer either: $GITHUB_REPOSITORY is the caller,
// and github.workflow_ref names the caller's TOP-LEVEL workflow, not the
// reusable workflow that is executing (only the OIDC job_workflow_ref
// claim carries that, and it is not exposed as a context or env var).
//
// The module path is, so that is what this reads.
package selfrepo

import (
	"runtime/debug"
	"strings"
)

// Slug returns the "owner/repo" slug of this binary's own module (e.g.
// "diggsweden/reusable-ci"), read from the build's module path at runtime.
// A fork that renames its Go module gets its own slug for free; callers
// that need to override (vendoring, mirrors) take an explicit value ahead
// of this. Returns "" when the module path is unavailable, so callers can
// degrade rather than assert someone else's identity.
func Slug() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return SlugFromModulePath(bi.Main.Path)
}

// SlugFromModulePath turns a Go module path into the "owner/repo" slug used
// in workflow `uses:` lines: it drops the host segment and any trailing
// "/vN" major-version element. "github.com/diggsweden/reusable-ci/v3" →
// "diggsweden/reusable-ci"; "" for paths too short to carry a slug.
func SlugFromModulePath(modPath string) string {
	parts := strings.Split(modPath, "/")
	if n := len(parts); n > 0 {
		if last := parts[n-1]; len(last) > 1 && last[0] == 'v' && allDigits(last[1:]) {
			parts = parts[:n-1]
		}
	}

	if len(parts) < 2 {
		return ""
	}

	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// allDigits reports whether value is non-empty and all ASCII digits.
func allDigits(value string) bool {
	if value == "" {
		return false
	}

	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}

	return true
}
