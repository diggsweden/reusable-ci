// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package platform detects which CI platform the binary is running on
// based on environment variables. Replaces the old scripts/ci/env.sh
// platform-detection branch.
//
// This is a thin lookup; no business logic, no I/O beyond os.Getenv.
package platform

import (
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Detect returns the active platform.
//
// Order: GITHUB_ACTIONS=true → GitHub; GITLAB_CI=true → GitLab; else Local.
// Local mode is what tests + the dev loop use.
func Detect() provider.Platform {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		return provider.PlatformGitHub
	case os.Getenv("GITLAB_CI") == "true":
		return provider.PlatformGitLab
	default:
		return provider.PlatformLocal
	}
}
