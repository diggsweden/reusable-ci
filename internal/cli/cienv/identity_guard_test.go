// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func TestProvenanceIdentity_UsesOnlyActiveRunner(t *testing.T) {
	for _, runner := range []string{"github", "forgejo", "gitlab", "local", "forgejo aliases"} {
		t.Run(runner, func(t *testing.T) {
			testenv.New(t)

			values := map[string]string{"GITHUB_SERVER_URL": "https://github.invalid", "GITHUB_REPOSITORY": "gh/repo", "GITHUB_RUN_ID": "1", "GITHUB_WORKFLOW_REF": "gh/workflow@ref", "FORGEJO_SERVER_URL": "https://forgejo.invalid", "FORGEJO_REPOSITORY": "fj/repo", "FORGEJO_RUN_ID": "2", "FORGEJO_WORKFLOW_REF": "fj/workflow@ref", "CI_SERVER_URL": "https://gitlab.invalid", "CI_PROJECT_PATH": "gl/repo", "CI_JOB_URL": "https://gitlab.invalid/jobs/3", "REPOSITORY": "wrong/repo", "CI_RUN_ID": "wrong"}
			for key, value := range values {
				t.Setenv(key, value)
			}

			wantBuilder, wantRun := "", ""

			switch runner {
			case "github":
				t.Setenv("GITHUB_ACTIONS", "true")

				wantBuilder = "https://github.invalid/gh/workflow@ref"
				wantRun = "https://github.invalid/gh/repo/actions/runs/1"
			case "forgejo":
				t.Setenv("GITHUB_ACTIONS", "true")
				t.Setenv("FORGEJO_ACTIONS", "true")

				wantBuilder = "https://forgejo.invalid/fj/workflow@ref"
				wantRun = "https://forgejo.invalid/fj/repo/actions/runs/2"
			case "gitlab":
				t.Setenv("GITLAB_CI", "true")

				wantBuilder = "https://gitlab.invalid/jobs/3"
				wantRun = wantBuilder
			case "forgejo aliases":
				t.Setenv("GITHUB_ACTIONS", "true")
				t.Setenv("GITEA_ACTIONS", "true")

				for _, key := range []string{"FORGEJO_SERVER_URL", "FORGEJO_REPOSITORY", "FORGEJO_RUN_ID", "FORGEJO_WORKFLOW_REF"} {
					t.Setenv(key, "")
				}

				wantBuilder = "https://github.invalid/gh/workflow@ref"
				wantRun = "https://github.invalid/gh/repo/actions/runs/1"
			}

			for _, target := range []string{"github", "forgejo", "gitlab", "local"} {
				t.Run(target, func(t *testing.T) {
					t.Setenv("REUSABLE_CI_PROVIDER", target)
					// A dialect override is not evidence that a build ran there.
					t.Setenv("REUSABLE_CI_RUNNER", target)
					require.Equal(t, wantBuilder, cienv.ProvenanceBuilderID())
					require.Equal(t, wantRun, cienv.ProvenanceInvocationID())
				})
			}
		})
	}
}
