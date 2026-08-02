// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
)

func TestProvenanceBuilderID_CanonicalWorkflowRef(t *testing.T) {
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
	t.Setenv("CI_JOB_URL", "")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_RUN_ID", "4242")

	const want = "https://github.com/o/r/actions/runs/4242"
	if got := cienv.ProvenanceInvocationID(); got != want {
		t.Errorf("invocation id = %q, want %q", got, want)
	}
}

func TestSourceDateEpochRFC3339(t *testing.T) {
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
	t.Setenv("GITHUB_WORKFLOW_REF", "")
	t.Setenv("GITHUB_SERVER_URL", "")
	t.Setenv("CI_JOB_URL", "https://gitlab.example/o/r/-/jobs/9")

	if got := cienv.ProvenanceBuilderID(); got != "https://gitlab.example/o/r/-/jobs/9" {
		t.Errorf("builder id = %q, want GitLab job URL fallback", got)
	}
}
