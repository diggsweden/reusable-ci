// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"context"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// pullRefPattern extracts the PR number from a refs/pull/<n>/{head,merge} ref.
var pullRefPattern = regexp.MustCompile(`^refs/pull/(\d+)/`)

// ResolveContext reads the Forgejo Actions env vars and returns a typed
// EventContext. No network, no I/O. Forgejo's act_runner mirrors the
// runner context into both FORGEJO_* and GITHUB_* variables; we prefer
// the FORGEJO_* names and fall back to the GITHUB_* aliases.
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	base := p.envFunc()
	// get reads FORGEJO_<suffix>, falling back to GITHUB_<suffix>.
	get := func(suffix string) string {
		return firstNonEmpty(base, "FORGEJO_"+suffix, "GITHUB_"+suffix)
	}

	sha := get("SHA")

	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	refName := get("REF_NAME")
	refType := classifyRefType(get)

	branch := get("HEAD_REF") // populated on pull_request events
	if branch == "" && refType == provider.RefTypeBranch {
		branch = refName
	}

	prNumber := ""

	if refType == provider.RefTypePR {
		if m := pullRefPattern.FindStringSubmatch(get("REF")); len(m) == 2 {
			prNumber = m[1]
		}
	}

	repo := get("REPOSITORY")

	server := get("SERVER_URL")
	if server == "" {
		server = defaultServer
	}

	repoURL := ""
	if repo != "" {
		repoURL = strings.TrimRight(server, "/") + "/" + repo
	}

	return &provider.EventContext{
		Platform:  provider.PlatformForgejo,
		RefName:   refName,
		RefType:   refType,
		SHA:       sha,
		ShortSHA:  short,
		Branch:    branch,
		PRNumber:  prNumber,
		EventName: get("EVENT_NAME"),
		Repo:      repo,
		RepoURL:   repoURL,
	}, nil
}

// classifyRefType maps the runner's REF_TYPE / EVENT_NAME / REF (passed
// via the suffix-aware get) to a provider.RefType. pull_request events
// override REF_TYPE (which reports "branch" on PRs); when REF_TYPE is
// unset we infer from the refs/{tags,heads,pull}/ prefix.
func classifyRefType(get func(string) string) provider.RefType {
	if strings.HasPrefix(get("EVENT_NAME"), "pull_request") {
		return provider.RefTypePR
	}

	switch get("REF_TYPE") {
	case "tag":
		return provider.RefTypeTag
	case "branch":
		return provider.RefTypeBranch
	}

	ref := get("REF")
	switch {
	case strings.HasPrefix(ref, "refs/tags/"):
		return provider.RefTypeTag
	case strings.HasPrefix(ref, "refs/heads/"):
		return provider.RefTypeBranch
	case strings.HasPrefix(ref, "refs/pull/"):
		return provider.RefTypePR
	default:
		return provider.RefTypeOther
	}
}
