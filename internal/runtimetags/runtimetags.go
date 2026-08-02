// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package runtimetags is the single definition of how reusable-ci
// runtime-image references (`reusable-ci-runtime-*:vX.Y.Z`) are
// recognised and rewritten across the workflow files. Two consumers
// share it so they cannot drift apart:
//
//   - cmd/bump-runtime-tags rewrites every pinned version on a release
//     cut (`just bump-runtime-tags <version>`).
//   - the TestRuntimeImageTagsShareOneVersion guard asserts all pinned
//     versions agree, so a missed site fails the build instead of
//     silently running a stale runtime.
//
// The runtime images are this repo's own release artifact — Renovate
// never bumps them, which is why both the rewriter and the guard exist.
package runtimetags

import "regexp"

// RefPattern matches any reusable-ci runtime image reference and
// captures its tag, e.g. "reusable-ci-runtime-java-25:v3.0.0" → "v3.0.0".
// Local-only tags such as ":verify" are matched too; consumers that
// only care about release pins filter with VersionPattern.
//
//nolint:gochecknoglobals // shared compiled pattern — read-only.
var RefPattern = regexp.MustCompile(`reusable-ci-runtime[a-z0-9-]*:([A-Za-z0-9._-]+)`)

// VersionPattern is the shape every published runtime tag must have:
// the repo's own release version.
//
//nolint:gochecknoglobals // shared compiled pattern — read-only.
var VersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// pinnedRefPattern matches only version-pinned references (never the
// local ":verify" build tags), splitting the ref stem from the version.
//
//nolint:gochecknoglobals // shared compiled pattern — read-only.
var pinnedRefPattern = regexp.MustCompile(`(reusable-ci-runtime[a-z0-9-]*:)v\d+\.\d+\.\d+`)

// Rewrite replaces the version of every pinned runtime-image reference
// in content with version, returning the new content and the number of
// references rewritten. Non-version tags (":verify") are untouched.
// The caller validates version against VersionPattern first.
func Rewrite(content, version string) (string, int) {
	count := 0
	rewritten := pinnedRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		count++

		stem := pinnedRefPattern.FindStringSubmatch(match)[1]

		return stem + version
	})

	return rewritten, count
}
