// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func TestProvenanceBuilderID_CanonicalWorkflowRef(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_WORKFLOW_REF", "diggsweden/reusable-ci/.github/workflows/release-binary.yml@refs/tags/v1.2.3")
	// Display name MUST NOT be used — that is the bug this guards against.
	t.Setenv("GITHUB_WORKFLOW", "Release binary")

	const want = "https://github.com/diggsweden/reusable-ci/.github/workflows/release-binary.yml@refs/tags/v1.2.3"
	if got := cienv.ProvenanceBuilderID(); got != want {
		t.Errorf("builder id = %q, want canonical workflow ref %q", got, want)
	}
}

func TestProvenanceInvocationID_RunURL(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("CI_JOB_URL", "")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_RUN_ID", "4242")

	const want = "https://github.com/o/r/actions/runs/4242"
	if got := cienv.ProvenanceInvocationID(); got != want {
		t.Errorf("invocation id = %q, want %q", got, want)
	}
}

func TestSourceDateEpochRFC3339_ConvertsTheEpochAndRejectsMalformed(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		t.Setenv("SOURCE_DATE_EPOCH", "1700000000")

		got, ok := cienv.SourceDateEpochRFC3339()
		if !ok || got != "2023-11-14T22:13:20Z" {
			t.Errorf("got (%q, %v), want valid RFC3339", got, ok)
		}
	})

	t.Run("unset", func(t *testing.T) {
		t.Setenv("SOURCE_DATE_EPOCH", "")

		if got, ok := cienv.SourceDateEpochRFC3339(); ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		t.Setenv("SOURCE_DATE_EPOCH", "not-a-number")

		if _, ok := cienv.SourceDateEpochRFC3339(); ok {
			t.Error("malformed epoch should report ok=false")
		}
	})
}

func TestProvenanceBuilderID_FallsBackToJobURL(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("GITHUB_WORKFLOW_REF", "")
	t.Setenv("GITHUB_SERVER_URL", "")
	t.Setenv("CI_JOB_URL", "https://gitlab.example/o/r/-/jobs/9")

	if got := cienv.ProvenanceBuilderID(); got != "https://gitlab.example/o/r/-/jobs/9" {
		t.Errorf("builder id = %q, want GitLab job URL fallback", got)
	}
}

func TestProvenanceIdentity_FallbacksDoNotBorrowTargetContext(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gitlab  bool
		missing string
		want    string
	}{
		{name: "github run", want: "https://github.invalid/runner/repo/actions/runs/42"},
		{name: "missing server", missing: "GITHUB_SERVER_URL"},
		{name: "missing repository", missing: "GITHUB_REPOSITORY"},
		{name: "missing run", missing: "GITHUB_RUN_ID"},
		{name: "gitlab pipeline", gitlab: true, want: "https://gitlab.invalid/runner/repo/-/pipelines/73"},
		{name: "missing pipeline", gitlab: true, missing: "CI_PIPELINE_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testenv.New(t)

			for key, value := range map[string]string{
				"REUSABLE_CI_PROVIDER": "forgejo",
				"GITHUB_SERVER_URL":    "https://github.invalid///", "GITHUB_REPOSITORY": "runner/repo", "GITHUB_RUN_ID": "42",
				"GITHUB_WORKFLOW":    "Display name is not an identity",
				"FORGEJO_SERVER_URL": "https://target.invalid", "FORGEJO_REPOSITORY": "target/repo",
				"FORGEJO_RUN_ID": "999", "FORGEJO_WORKFLOW_REF": "target/workflow@wrong",
				"FORGEJO_SERVER": "https://alias.invalid", "FORGEJO_REPO": "alias/repo",
				"CI_SERVER_URL": "https://gitlab.invalid", "CI_PROJECT_PATH": "runner/repo", "CI_PIPELINE_ID": "73",
				"CI_PIPELINE_URL": "https://gitlab.invalid/runner/repo/-/pipelines/73",
				"CI_RUN_ID":       "neutral-run", "CI_RUN_URL": "https://neutral.invalid/run", "REPOSITORY": "neutral/repo",
			} {
				t.Setenv(key, value)
			}

			if tc.gitlab {
				t.Setenv("GITLAB_CI", "true")
			} else {
				t.Setenv("GITHUB_ACTIONS", "true")
			}

			if tc.missing != "" {
				t.Setenv(tc.missing, "")
			}

			if got := cienv.ProvenanceBuilderID(); got != tc.want {
				t.Errorf("builder = %q, want %q", got, tc.want)
			}

			if got := cienv.ProvenanceInvocationID(); got != tc.want {
				t.Errorf("invocation = %q, want %q", got, tc.want)
			}
		})
	}
}
