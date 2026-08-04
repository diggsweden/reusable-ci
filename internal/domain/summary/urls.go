// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Link routing sits behind provider.WebURLBuilder, not in a platform switch
// here: that GitLab inserts "/-/" and that Forgejo hangs packages off the owner
// is forge knowledge, and a fourth forge belongs in an adapter rather than in an
// edit to this file. What remains in the domain is what to render when there is
// no hosted page to link to.

// ReleaseURL links to the release page for version, or returns a textual
// placeholder when the platform has no web UI to link to (builder is nil).
func ReleaseURL(builder provider.WebURLBuilder, server, repo, version string) string {
	if builder == nil {
		return fmt.Sprintf("(release: %s)", version)
	}

	return builder.ReleaseWebURL(server, repo, version)
}

// PackagesURL links to the packages page, or returns a textual placeholder when
// the platform has no web UI to link to (builder is nil).
func PackagesURL(builder provider.WebURLBuilder, server, repo string) string {
	if builder == nil {
		return "(packages)"
	}

	return builder.PackagesWebURL(server, repo)
}
