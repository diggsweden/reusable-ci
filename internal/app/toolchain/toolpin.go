// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"strings"

	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// The toolchain has one immutable-version policy, stated here and used by every
// rule that pins a tool: the mise binary, the changelog renderer, the Rust
// channel, and every tool version declared in a mise config or lock.
//
// The core is an exact MAJOR.MINOR.PATCH release — the same rule the release
// verbs apply to a tag. Anything a tool manager would have to resolve at
// install time is not a pin: an alias like latest or lts, a partial version, a
// range, a ref or prefix selector, or a path. Those all make the installed
// bytes a function of when the install ran, which is what pinning exists to
// remove.
//
// Two spellings differ, and they differ by where the value is consumed:
//
//   - A download URL embeds the version literally, so the mise binary and the
//     changelog renderer take the bare core and nothing else.
//   - A registry entry is a tag, so a declared tool version may carry the "v"
//     that tag spelling uses, and a prerelease or build suffix. Those still
//     name one immutable release rather than a set to choose from.

// exactDownloadVersion reports whether a value may be interpolated into a
// release download URL: a bare MAJOR.MINOR.PATCH with no prefix or suffix.
func exactDownloadVersion(value string) bool {
	return domainversion.IsStableSemverTag("v" + value)
}

// immutableToolPinValue reports whether a declared tool version names exactly
// one release, in either spelling a registry hands back.
func immutableToolPinValue(value string) bool {
	core, suffix := splitReleaseSuffix(value)
	if !exactDownloadVersion(domainversion.StripVPrefix(core)) || strings.HasPrefix(core, "vv") {
		return false
	}

	return validReleaseSuffix(suffix)
}

// splitReleaseSuffix separates the release core from any prerelease or build
// metadata, keeping the separator with the suffix so an empty one is visible.
func splitReleaseSuffix(value string) (string, string) {
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		return value[:index], value[index:]
	}

	return value, ""
}

// validReleaseSuffix accepts the SemVer prerelease and build alphabet, including
// the separator between them, so a suffix cannot smuggle a selector, a path
// separator or control text past the core check.
func validReleaseSuffix(suffix string) bool {
	if suffix == "" {
		return true
	}

	body := suffix[1:]
	if body == "" {
		return false
	}

	for _, char := range body {
		switch {
		case char >= '0' && char <= '9',
			char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char == '.', char == '-', char == '+':
		default:
			return false
		}
	}

	return true
}
