// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// pullRefPattern extracts the PR number from a refs/pull/<n>/{head,merge} ref.
var pullRefPattern = regexp.MustCompile(`^refs/pull/(\d+)/`)

// ResolveContext reads the canonical GitHub Actions env vars and returns
// a typed EventContext. No network, no I/O.
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.envFunc()

	sha := get("GITHUB_SHA")

	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	refName := get("GITHUB_REF_NAME")
	refType := classifyRefType(get)

	branch := get("GITHUB_HEAD_REF") // populated on pull_request events
	if branch == "" && refType == provider.RefTypeBranch {
		branch = refName
	}

	prNumber := ""

	if refType == provider.RefTypePR {
		if m := pullRefPattern.FindStringSubmatch(get("GITHUB_REF")); len(m) == 2 {
			prNumber = m[1]
		}
	}

	repo := get("GITHUB_REPOSITORY")

	server := get("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}

	repoURL := ""
	if repo != "" {
		repoURL = strings.TrimRight(server, "/") + "/" + repo
	}

	return &provider.EventContext{
		Platform:  provider.PlatformGitHub,
		RefName:   refName,
		RefType:   refType,
		SHA:       sha,
		ShortSHA:  short,
		Branch:    branch,
		PRNumber:  prNumber,
		EventName: get("GITHUB_EVENT_NAME"),
		Repo:      repo,
		RepoURL:   repoURL,
	}, nil
}

// classifyRefType maps GITHUB_REF_TYPE / GITHUB_REF / GITHUB_EVENT_NAME
// to one of provider.RefType{Branch,Tag,PR,Other}.
//
// GitHub's GITHUB_REF_TYPE only ever returns "branch" or "tag"; it
// reports "branch" on pull_request events too. We override that to PR
// when the event is a pull_request{,_target,_review,…} so the
// EventContext distinguishes the three cases the rest of the code
// needs.
func classifyRefType(get func(string) string) provider.RefType {
	if strings.HasPrefix(get("GITHUB_EVENT_NAME"), "pull_request") {
		return provider.RefTypePR
	}

	switch get("GITHUB_REF_TYPE") {
	case "tag":
		return provider.RefTypeTag
	case "branch":
		return provider.RefTypeBranch
	}

	return provider.RefTypeOther
}
