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

import (
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

//nolint:gochecknoglobals // shared compiled pattern — read-only.
var refPattern = regexp.MustCompile("reusable-ci-runtime(?:-[a-z0-9]+)*:([^\\s\"'`,;(){}\\[\\]<>]+)")

// ValidVersion reports whether a published runtime tag is the repository's
// stable v-prefixed semantic-version shape.
func ValidVersion(value string) bool { return version.IsStableSemverTag(value) }

func references(content string) [][]int {
	var found [][]int

	for _, match := range refPattern.FindAllStringSubmatchIndex(content, -1) {
		if match[0] > 0 && !strings.ContainsRune("/ \t\r\n\"'`=([{", rune(content[match[0]-1])) {
			continue
		}

		found = append(found, match)
	}

	return found
}

// Tags returns complete runtime-reference tags, including nonstable or invalid
// pins so validators cannot mistake a stable prefix for a valid full token.
func Tags(content string) []string {
	refs := references(content)

	tags := make([]string, 0, len(refs))
	for _, match := range refs {
		tags = append(tags, content[match[2]:match[3]])
	}

	return tags
}

// Rewrite replaces the version of every pinned runtime-image reference
// in content with version, returning the new content and the number of
// references rewritten. Non-version tags (":verify") are untouched.
// The caller validates version with ValidVersion first.
func Rewrite(content, version string) (string, int) {
	count := 0

	var rewritten strings.Builder

	offset := 0

	for _, match := range references(content) {
		if !ValidVersion(content[match[2]:match[3]]) {
			continue
		}

		rewritten.WriteString(content[offset:match[2]])
		rewritten.WriteString(version)

		offset = match[3]
		count++
	}

	rewritten.WriteString(content[offset:])

	return rewritten.String(), count
}
