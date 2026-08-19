// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestClassifyRefType covers the GitHub half of a rule the three
// providers share: GITHUB_REF_TYPE reports "branch" during a
// pull_request event, so the event-name override is the only thing that
// keeps a PR off the trusted branch-push path.
//
// The forgejo adapter has the same override and the gitlab one its
// CI_PIPELINE_SOURCE equivalent. This classifier had no direct test.
func TestClassifyRefType(t *testing.T) {
	t.Parallel()

	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}

	for _, tc := range []struct {
		name string
		vars map[string]string
		want provider.RefType
	}{
		{
			name: "pull_request wins over a branch REF_TYPE",
			vars: map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF_TYPE": "branch"},
			want: provider.RefTypePR,
		},
		{
			// pull_request_target runs with repository secrets available,
			// so misclassifying it as a branch push is the costliest of
			// these cases. The prefix match is what covers it.
			name: "pull_request_target is still a PR",
			vars: map[string]string{"GITHUB_EVENT_NAME": "pull_request_target", "GITHUB_REF_TYPE": "branch"},
			want: provider.RefTypePR,
		},
		{
			name: "pull_request_review is still a PR",
			vars: map[string]string{"GITHUB_EVENT_NAME": "pull_request_review", "GITHUB_REF_TYPE": "branch"},
			want: provider.RefTypePR,
		},
		{
			name: "tag push",
			vars: map[string]string{"GITHUB_EVENT_NAME": "push", "GITHUB_REF_TYPE": "tag"},
			want: provider.RefTypeTag,
		},
		{
			name: "branch push",
			vars: map[string]string{"GITHUB_EVENT_NAME": "push", "GITHUB_REF_TYPE": "branch"},
			want: provider.RefTypeBranch,
		},
		{
			// Unlike the forgejo adapter there is no ref-prefix fallback,
			// because GITHUB_REF_TYPE is always set on GitHub Actions.
			// Anything else is "other" rather than guessed into a class
			// that carries trust.
			name: "no ref type at all",
			vars: map[string]string{"GITHUB_EVENT_NAME": "schedule"},
			want: provider.RefTypeOther,
		},
		{
			name: "unknown ref type",
			vars: map[string]string{"GITHUB_REF_TYPE": "sideways"},
			want: provider.RefTypeOther,
		},
		{
			// A ref that looks like a tag does not make it one here.
			name: "ref alone is not consulted",
			vars: map[string]string{"GITHUB_REF": "refs/tags/v1.2.3"},
			want: provider.RefTypeOther,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := classifyRefType(env(tc.vars)); got != tc.want {
				t.Errorf("classifyRefType(%v) = %v, want %v", tc.vars, got, tc.want)
			}
		})
	}
}
