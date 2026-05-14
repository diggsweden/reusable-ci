// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// ReleaseURL builds the platform-aware URL to a release page. Mirrors
// ci_release_url in scripts/ci/output.sh.
//
//	github → <server>/<repo>/releases/tag/<version>
//	gitlab → <server>/<repo>/-/releases/<version>
//	other  → "(release: <version>)" placeholder
func ReleaseURL(plat provider.Platform, server, repo, version string) string {
	switch plat {
	case provider.PlatformGitHub:
		return fmt.Sprintf("%s/%s/releases/tag/%s", server, repo, version)
	case provider.PlatformGitLab:
		return fmt.Sprintf("%s/%s/-/releases/%s", server, repo, version)
	}
	return fmt.Sprintf("(release: %s)", version)
}

// PackagesURL builds the platform-aware URL to a packages page.
//
//	github → <server>/<repo>/packages
//	gitlab → <server>/<repo>/-/packages
//	other  → "(packages)" placeholder
func PackagesURL(plat provider.Platform, server, repo string) string {
	switch plat {
	case provider.PlatformGitHub:
		return fmt.Sprintf("%s/%s/packages", server, repo)
	case provider.PlatformGitLab:
		return fmt.Sprintf("%s/%s/-/packages", server, repo)
	}
	return "(packages)"
}
