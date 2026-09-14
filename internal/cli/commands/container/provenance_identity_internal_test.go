// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// This tests the command's input builder, not an invocation of container attest.
func TestProvenanceFromEnv_RunnerTargetIdentity(t *testing.T) {
	const image = "registry.invalid/publish/app@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	for _, tc := range []struct {
		name, target, marker, server, repo, ref, sha, builder, invocation string
		actions, aliases                                                  bool
	}{
		{"github", "forgejo", "", "https://github.invalid", "gh/source", "v1.2.3", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "https://github.invalid/gh/source/.github/workflows/build.yml@refs/tags/v1.2.3", "https://github.invalid/gh/source/actions/runs/11", true, false},
		{"forgejo", "gitlab", "FORGEJO_ACTIONS", "https://forgejo.invalid", "fj/source", "v4.5.6", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "https://forgejo.invalid/fj/source/.forgejo/workflows/build.yml@refs/tags/v4.5.6", "https://forgejo.invalid/fj/source/actions/runs/22", true, false},
		{"compatibility aliases", "github", "GITEA_ACTIONS", "https://github.invalid", "gh/source", "v1.2.3", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "https://github.invalid/gh/source/.github/workflows/build.yml@refs/tags/v1.2.3", "https://github.invalid/gh/source/actions/runs/11", true, true},
		{"gitlab", "forgejo", "GITLAB_CI", "https://gitlab.invalid", "gl/source", "v7.8.9", "cccccccccccccccccccccccccccccccccccccccc", "https://gitlab.invalid/gl/source/-/jobs/33", "https://gitlab.invalid/gl/source/-/jobs/33", false, false},
		{name: "local", target: "forgejo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testenv.New(t)

			for key, value := range map[string]string{
				"GITHUB_SERVER_URL": "https://github.invalid///", "GITHUB_REPOSITORY": "gh/source", "GITHUB_REF_NAME": "v1.2.3", "GITHUB_REF": "refs/tags/wrong-full-ref", "GITHUB_SHA": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"GITHUB_WORKFLOW_REF": "gh/source/.github/workflows/build.yml@refs/tags/v1.2.3", "GITHUB_WORKFLOW": "Wrong display name", "GITHUB_RUN_ID": "11",
				"FORGEJO_SERVER_URL": "https://forgejo.invalid///", "FORGEJO_REPOSITORY": "fj/source", "FORGEJO_REF_NAME": "v4.5.6", "FORGEJO_SHA": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"FORGEJO_WORKFLOW_REF": "fj/source/.forgejo/workflows/build.yml@refs/tags/v4.5.6", "FORGEJO_RUN_ID": "22",
				"CI_SERVER_URL": "https://gitlab.invalid///", "CI_PROJECT_PATH": "gl/source", "CI_COMMIT_REF_NAME": "v7.8.9", "CI_COMMIT_SHA": "cccccccccccccccccccccccccccccccccccccccc",
				"CI_JOB_URL": "https://gitlab.invalid/gl/source/-/jobs/33", "CI_PIPELINE_URL": "https://gitlab.invalid/gl/source/-/pipelines/44",
				"REPOSITORY": "neutral/wrong", "CI_REPO": "neutral/wrong", "REF_NAME": "wrong-neutral-ref", "CI_REF_NAME": "wrong-ci-ref",
				"CI_COMMIT": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "COMMIT_SHA": "ffffffffffffffffffffffffffffffffffffffff",
				"FORGEJO_SERVER": "https://alias.invalid", "FORGEJO_REPO": "alias/wrong", "CI_RUN_ID": "wrong-neutral-run",
				"SOURCE_DATE_EPOCH": "1700000000", "BUILD_STARTED_ON": "", "BUILD_FINISHED_ON": "",
			} {
				t.Setenv(key, value)
			}

			t.Setenv("REUSABLE_CI_PROVIDER", tc.target)
			t.Setenv("REUSABLE_CI_RUNNER", tc.target)

			if tc.actions {
				t.Setenv("GITHUB_ACTIONS", "true")
			}

			if tc.marker != "" {
				t.Setenv(tc.marker, "true")
			}

			if tc.aliases {
				for _, key := range []string{"FORGEJO_SERVER_URL", "FORGEJO_REPOSITORY", "FORGEJO_REF_NAME", "FORGEJO_SHA", "FORGEJO_WORKFLOW_REF", "FORGEJO_RUN_ID"} {
					t.Setenv(key, "")
				}
			}

			want := provenance.Input{
				BuildType: "https://diggsweden.github.io/reusable-ci/container-build/v1",
				BuilderID: tc.builder, Ref: tc.ref, ImageName: image, InvocationID: tc.invocation,
				StartedOn: "2023-11-14T22:13:20Z", FinishedOn: "2023-11-14T22:13:20Z",
			}
			if tc.server != "" {
				want.SourceURI = "git+" + tc.server + "/" + tc.repo
				want.ResolvedDeps = []provenance.Dependency{{
					URI: want.SourceURI + "@" + tc.ref, DigestType: "gitCommit", Digest: tc.sha,
				}}
			}

			require.Equal(t, want, provenanceFromEnv(image))
		})
	}
}
