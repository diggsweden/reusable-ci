// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Named so the package's goconst budget is not spent on test
// fixtures repeating the runner's own vocabulary.
const (
	refTypeBranch     = "branch"
	mergeRequestEvent = "merge_request_event"
)

// TestClassifyRefType covers the gitlab half of the shared rule. The PR
// case was reached through the provider tests; the rest of the ladder
// was not, and the ladder is ordered for a reason.
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
			// Overrides everything below: a merge-request pipeline sets
			// CI_COMMIT_REF_NAME (and may set CI_COMMIT_BRANCH), so
			// without this first the branch rungs would claim it.
			name: "merge request wins over the branch rungs",
			vars: map[string]string{
				"CI_PIPELINE_SOURCE": mergeRequestEvent,
				"CI_COMMIT_BRANCH":   "feature",
				"CI_COMMIT_REF_NAME": "feature",
			},
			want: provider.RefTypePR,
		},
		{
			// Exact match, unlike the github and forgejo prefix tests:
			// gitlab's pipeline sources are a closed set of exact values,
			// and mergeRequestEvent is the only one that means a MR.
			name: "another pipeline source is not a merge request",
			vars: map[string]string{"CI_PIPELINE_SOURCE": "push", "CI_COMMIT_BRANCH": "main"},
			want: provider.RefTypeBranch,
		},
		{
			// A tag pipeline also populates CI_COMMIT_REF_NAME, so tag
			// has to be checked before the branch rungs.
			name: "tag outranks the ref name",
			vars: map[string]string{"CI_COMMIT_TAG": "v1.2.3", "CI_COMMIT_REF_NAME": "v1.2.3"},
			want: provider.RefTypeTag,
		},
		{
			name: refTypeBranch,
			vars: map[string]string{"CI_COMMIT_BRANCH": "main", "CI_COMMIT_REF_NAME": "main"},
			want: provider.RefTypeBranch,
		},
		{
			// Detached-HEAD pipelines leave CI_COMMIT_BRANCH empty.
			name: "ref name alone still counts as a branch",
			vars: map[string]string{"CI_COMMIT_REF_NAME": "main"},
			want: provider.RefTypeBranch,
		},
		{
			name: "nothing set",
			vars: map[string]string{},
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
