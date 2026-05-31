// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveContext reads the canonical GitLab CI env vars and returns a
// typed EventContext. No network, no I/O.
//
// Mapping:
//
//	CI_COMMIT_REF_NAME       → RefName
//	CI_COMMIT_TAG (presence) → RefType=Tag, else RefType=Branch
//	CI_PIPELINE_SOURCE=merge_request_event → RefType=PR
//	CI_COMMIT_SHA            → SHA
//	CI_COMMIT_SHORT_SHA      → ShortSHA  (CI_COMMIT_SHA[:7] fallback)
//	CI_COMMIT_BRANCH || CI_MERGE_REQUEST_SOURCE_BRANCH_NAME → Branch
//	CI_MERGE_REQUEST_IID     → PRNumber
//	CI_PIPELINE_SOURCE       → EventName
//	CI_PROJECT_PATH          → Repo
//	CI_PROJECT_URL           → RepoURL
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.Env
	if get == nil {
		get = os.Getenv
	}

	sha := get("CI_COMMIT_SHA")

	short := get("CI_COMMIT_SHORT_SHA")
	if short == "" && len(sha) > 7 {
		short = sha[:7]
	}

	refType := classifyRefType(get)

	branch := get("CI_COMMIT_BRANCH")
	if branch == "" {
		branch = get("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")
	}

	prNumber := get("CI_MERGE_REQUEST_IID")

	return &provider.EventContext{
		Platform:  provider.PlatformGitLab,
		RefName:   get("CI_COMMIT_REF_NAME"),
		RefType:   refType,
		SHA:       sha,
		ShortSHA:  short,
		Branch:    branch,
		PRNumber:  prNumber,
		EventName: get("CI_PIPELINE_SOURCE"),
		Repo:      get("CI_PROJECT_PATH"),
		RepoURL:   get("CI_PROJECT_URL"),
	}, nil
}

// classifyRefType resolves the ref type from a GitLab CI env snapshot.
// Order of precedence:
//
//  1. CI_PIPELINE_SOURCE=merge_request_event → PR (overrides everything)
//  2. CI_COMMIT_TAG present                  → Tag
//  3. CI_COMMIT_BRANCH present               → Branch
//  4. CI_COMMIT_REF_NAME present             → Branch (fallback when
//     CI_COMMIT_BRANCH isn't populated, e.g. detached HEAD pipelines)
//  5. otherwise                              → Other
func classifyRefType(get func(string) string) provider.RefType {
	if get("CI_PIPELINE_SOURCE") == "merge_request_event" {
		return provider.RefTypePR
	}

	if get("CI_COMMIT_TAG") != "" {
		return provider.RefTypeTag
	}

	if get("CI_COMMIT_BRANCH") != "" {
		return provider.RefTypeBranch
	}

	if get("CI_COMMIT_REF_NAME") != "" {
		return provider.RefTypeBranch
	}

	return provider.RefTypeOther
}
