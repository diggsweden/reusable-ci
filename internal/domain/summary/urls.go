// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// The summary used to switch on the platform to build these links, which meant
// the domain knew that GitLab inserts "/-/" and that Forgejo hangs packages off
// the owner. That is forge knowledge, and a fourth forge would have arrived as
// an edit here rather than as an adapter.
//
// Now the routing sits behind provider.WebURLBuilder and what remains in the
// domain is the only part that is genuinely its own: what to render when there
// is no hosted page to link to.

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
