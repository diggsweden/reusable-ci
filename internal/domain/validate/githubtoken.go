// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import "strings"

// GitHubTokenKind classifies a GitHub token by prefix. The release-bot
// authorization policy is:
//
//   - github_pat_*: fine-grained PAT (preferred for release bot)
//   - ghs_*:        GitHub App installation token
//   - ghp_*:        classic PAT (overly broad — must error)
//   - anything else: unknown, surface a warning
type GitHubTokenKind int

const (
	// GitHubTokenUnknown — no recognised prefix.
	GitHubTokenUnknown GitHubTokenKind = iota
	// GitHubTokenClassic — ghp_* (refused).
	GitHubTokenClassic
	// GitHubTokenFineGrained — github_pat_* (preferred).
	GitHubTokenFineGrained
	// GitHubTokenApp — ghs_* (installation token from a GitHub App).
	GitHubTokenApp
)

// ClassifyGitHubToken inspects the token's prefix.
func ClassifyGitHubToken(token string) GitHubTokenKind {
	switch {
	case strings.HasPrefix(token, "github_pat_"):
		return GitHubTokenFineGrained
	case strings.HasPrefix(token, "ghs_"):
		return GitHubTokenApp
	case strings.HasPrefix(token, "ghp_"):
		return GitHubTokenClassic
	}

	return GitHubTokenUnknown
}
