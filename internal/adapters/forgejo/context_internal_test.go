// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestClassifyRefType covers all seven branches; roughly a third were
// exercised before.
//
// The precedence is the part that matters. A Forgejo runner reports
// REF_TYPE="branch" during a pull_request event, so without the
// event-name override every PR would classify as a branch push — and a
// branch push is the trusted context that a PR deliberately is not.
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
			// The override, and the reason it exists: REF_TYPE says
			// "branch" and the ref looks like a branch, yet this is a PR.
			name: "pull_request wins over a branch REF_TYPE",
			vars: map[string]string{"EVENT_NAME": "pull_request", "REF_TYPE": "branch", "REF": "refs/heads/feature"},
			want: provider.RefTypePR,
		},
		{
			// Forgejo emits pull_request_target and similar; the prefix
			// match is what keeps them all on the untrusted path.
			name: "pull_request_target is still a PR",
			vars: map[string]string{"EVENT_NAME": "pull_request_target", "REF_TYPE": "branch"},
			want: provider.RefTypePR,
		},
		{
			name: "explicit tag",
			vars: map[string]string{"REF_TYPE": "tag", "REF": "refs/tags/v1.2.3"},
			want: provider.RefTypeTag,
		},
		{
			name: "explicit branch",
			vars: map[string]string{"REF_TYPE": "branch", "REF": "refs/heads/main"},
			want: provider.RefTypeBranch,
		},

		// With REF_TYPE unset the ref prefix decides.
		{
			name: "inferred tag",
			vars: map[string]string{"REF": "refs/tags/v1.2.3"},
			want: provider.RefTypeTag,
		},
		{
			name: "inferred branch",
			vars: map[string]string{"REF": "refs/heads/main"},
			want: provider.RefTypeBranch,
		},
		{
			name: "inferred pull request",
			vars: map[string]string{"REF": "refs/pull/42/merge"},
			want: provider.RefTypePR,
		},

		// Anything else is "other" rather than guessed at: a release
		// decision made on a ref nobody recognised should not default
		// into a trusted class.
		{
			name: "unrecognised ref",
			vars: map[string]string{"REF": "refs/notes/commits"},
			want: provider.RefTypeOther,
		},
		{
			name: "nothing set at all",
			vars: map[string]string{},
			want: provider.RefTypeOther,
		},
		{
			// A REF_TYPE the runner never sends falls through to the ref.
			name: "unknown REF_TYPE falls back to the ref",
			vars: map[string]string{"REF_TYPE": "sideways", "REF": "refs/tags/v1.2.3"},
			want: provider.RefTypeTag,
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
