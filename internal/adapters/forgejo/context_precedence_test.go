// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestResolveContext_ForgejoNamesWinOverCompetingAliases resolves complete
// runner environments where every GITHUB_* alias carries a different value
// from its FORGEJO_* name, and compares the whole EventContext. A pull request
// keeps its own number, head branch and event; a push whose alias claims a
// pull request stays a branch push named by its ref, with no PR number, even
// though the head-branch alias is set and the Forgejo name is empty.
func TestResolveContext_ForgejoNamesWinOverCompetingAliases(t *testing.T) {
	t.Parallel()

	aliases := map[string]string{
		"GITHUB_SHA": "ffffffffffffffffffffffffffffffffffffffff", "GITHUB_REF": "refs/pull/99/merge", "GITHUB_REF_NAME": "99/merge",
		"GITHUB_REF_TYPE": "tag", "GITHUB_HEAD_REF": "alias-branch", "GITHUB_EVENT_NAME": "pull_request_target",
		"GITHUB_REPOSITORY": "alias/repo", "GITHUB_SERVER_URL": "https://alias.invalid",
	}

	for name, tc := range map[string]struct {
		forgejo map[string]string
		want    provider.EventContext
	}{
		"pull request": {
			forgejo: map[string]string{
				"FORGEJO_SHA": "0123456789abcdef0123456789abcdef01234567", "FORGEJO_REF": "refs/pull/7/head", "FORGEJO_REF_NAME": "7/head",
				"FORGEJO_REF_TYPE": "branch", "FORGEJO_HEAD_REF": "feature/login", "FORGEJO_EVENT_NAME": "pull_request",
				"FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_SERVER_URL": "https://forgejo.invalid/",
			},
			want: provider.EventContext{
				ForgeAPI: provider.ForgeForgejo, RefName: "7/head", RefType: provider.RefTypePR,
				SHA: "0123456789abcdef0123456789abcdef01234567", ShortSHA: "0123456", Branch: "feature/login", PRNumber: "7",
				EventName: "pull_request", Repo: "owner/repo", RepoURL: "https://forgejo.invalid/owner/repo",
			},
		},
		"push despite a pull-request alias": {
			forgejo: map[string]string{
				"FORGEJO_SHA": "89abcdef0123456789abcdef0123456789abcdef", "FORGEJO_REF": "refs/heads/main", "FORGEJO_REF_NAME": "main",
				"FORGEJO_REF_TYPE": "branch", "FORGEJO_HEAD_REF": "", "FORGEJO_EVENT_NAME": "push",
				"FORGEJO_REPOSITORY": "owner/repo", "FORGEJO_SERVER_URL": "https://forgejo.invalid",
			},
			want: provider.EventContext{
				ForgeAPI: provider.ForgeForgejo, RefName: "main", RefType: provider.RefTypeBranch,
				SHA: "89abcdef0123456789abcdef0123456789abcdef", ShortSHA: "89abcde", Branch: "main",
				EventName: "push", Repo: "owner/repo", RepoURL: "https://forgejo.invalid/owner/repo",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{}
			for key, value := range aliases {
				env[key] = value
			}

			for key, value := range tc.forgejo {
				env[key] = value
			}

			got, err := (&forgejo.Provider{Env: envMap(env)}).ResolveContext(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.want, *got)
		})
	}
}
