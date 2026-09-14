// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Link routing sits behind provider.WebURLBuilder, not in a platform switch
// here: that GitLab inserts "/-/" and that Forgejo hangs packages off the owner
// is forge knowledge, and a fourth forge belongs in an adapter rather than in an
// edit to this file. What remains in the domain is what to render when there is
// no hosted page to link to.

// ReleaseURL links to the release page for version. It returns "" when there
// is no page to link to: the platform has no web UI (builder is nil), or the
// server, repository or version is missing.
//
// It used to return a textual placeholder such as "(release: v1.0.0)" instead,
// which every caller wrote into a Markdown link destination, so a summary on a
// platform without a web UI rendered "[Release]((release: v1.0.0))" -- a broken
// link, not a readable fallback. Deciding what to show in its place belongs to
// whoever renders the line.
func ReleaseURL(builder provider.WebURLBuilder, server, repo, version string) string {
	if builder == nil || server == "" || repo == "" || version == "" {
		return ""
	}

	return builder.ReleaseWebURL(server, repo, version)
}

// PackagesURL links to the packages page, or returns "" when there is no page
// to link to (no web UI, or no server or repository).
func PackagesURL(builder provider.WebURLBuilder, server, repo string) string {
	if builder == nil || server == "" || repo == "" {
		return ""
	}

	return builder.PackagesWebURL(server, repo)
}
