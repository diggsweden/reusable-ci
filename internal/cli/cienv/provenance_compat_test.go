// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/stretchr/testify/require"
)

// Forgejo Actions ships a GitHub-compatibility profile: a Forgejo runner sets
// GITHUB_ACTIONS=true and populates GITHUB_REPOSITORY, GITHUB_SERVER_URL,
// GITHUB_SHA and the rest, so that actions written for GitHub keep working.
//
// For provenance that compatibility is a hazard, because the values look
// exactly like GitHub's and describe something else entirely. A build on
// forgejo.example.com must not produce a SLSA statement whose source and
// builder say github.com — that is an attestation attributing an artifact to a
// repository on a forge it never touched, signed and published as evidence.
//
// The policy this pins: on a Forgejo runner the FORGEJO_ names win, and the
// GITHUB_ aliases are only ever a fallback for values Forgejo does not set
// under its own name. What the statement says is where the build actually ran.
//
// These use t.Setenv, so no t.Parallel.

// forgejoRunner is the compatibility profile as a Forgejo runner presents it:
// its own names AND the GitHub aliases, pointing at the same instance.
func forgejoRunner(t *testing.T) {
	t.Helper()

	for key, value := range map[string]string{
		"GITHUB_ACTIONS":     "true",
		"FORGEJO_ACTIONS":    "true",
		"FORGEJO_SERVER_URL": "https://forgejo.example.com",
		"FORGEJO_REPOSITORY": "team/app",
		"FORGEJO_REF_NAME":   "v1.2.3",
		"FORGEJO_SHA":        "1111111111111111111111111111111111111111",
		"FORGEJO_RUN_ID":     "77",
		"GITHUB_SERVER_URL":  "https://forgejo.example.com",
		"GITHUB_REPOSITORY":  "team/app",
		"GITHUB_REF_NAME":    "v1.2.3",
		"GITHUB_SHA":         "1111111111111111111111111111111111111111",
		"GITHUB_RUN_ID":      "77",
	} {
		t.Setenv(key, value)
	}
}

func clearForgeEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"GITHUB_ACTIONS", "GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "GITHUB_REF_NAME", "GITHUB_SHA", "GITHUB_RUN_ID",
		"FORGEJO_ACTIONS", "FORGEJO_SERVER_URL", "FORGEJO_REPOSITORY", "FORGEJO_REF_NAME", "FORGEJO_SHA",
		"FORGEJO_RUN_ID", "FORGEJO_OUTPUT", "GITEA_ACTIONS",
		"GITLAB_CI", "CI_PROJECT_PATH", "CI_COMMIT_REF_NAME", "CI_JOB_URL", "CI_PIPELINE_URL", "CI_SERVER_URL",
	} {
		t.Setenv(key, "")
	}
}

func TestProvenanceSource_OnForgejoNamesTheForgejoInstance(t *testing.T) {
	clearForgeEnv(t)
	forgejoRunner(t)

	repoURL, ref, commit := cienv.ProvenanceSource()

	require.Equal(t, "https://forgejo.example.com/team/app", repoURL,
		"the provenance source names a forge the build never ran on")
	require.Equal(t, "v1.2.3", ref)
	require.Equal(t, "1111111111111111111111111111111111111111", commit)
	require.NotContains(t, repoURL, "github.com")
}

// The case the compatibility profile makes possible: the GitHub aliases
// disagree with Forgejo's own names. That happens when an action or a workflow
// sets GITHUB_* itself — they are ordinary environment variables on a Forgejo
// runner, not runner-attested values. The attested Forgejo name must win.
func TestProvenanceSource_ForgejoNamesOutrankTheGitHubAliases(t *testing.T) {
	clearForgeEnv(t)
	forgejoRunner(t)

	// Something in the job overwrote the aliases to point at github.com.
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "attacker/repo")
	t.Setenv("GITHUB_SHA", "2222222222222222222222222222222222222222")

	repoURL, _, commit := cienv.ProvenanceSource()

	require.Equal(t, "https://forgejo.example.com/team/app", repoURL,
		"a provenance statement attributed the build to github.com/attacker/repo, which it never touched")
	require.Equal(t, "1111111111111111111111111111111111111111", commit,
		"the commit came from the overwritable alias rather than the runner's own name")
}

func TestProvenanceInvocationID_OnForgejoPointsAtTheForgejoRun(t *testing.T) {
	clearForgeEnv(t)
	forgejoRunner(t)

	require.Equal(t, "https://forgejo.example.com/team/app/actions/runs/77", cienv.ProvenanceInvocationID(),
		"the invocation URL must resolve on the forge that ran the build")
}

// A genuine GitHub runner must still resolve to github.com — otherwise the
// assertions above are satisfied by a resolver that simply never reports
// GitHub, which would be a different bug with the same test result.
func TestProvenanceSource_OnGitHubNamesGitHub(t *testing.T) {
	clearForgeEnv(t)

	for key, value := range map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "diggsweden/app",
		"GITHUB_REF_NAME":   "v2.0.0",
		"GITHUB_SHA":        "3333333333333333333333333333333333333333",
	} {
		t.Setenv(key, value)
	}

	repoURL, ref, commit := cienv.ProvenanceSource()

	require.Equal(t, "https://github.com/diggsweden/app", repoURL)
	require.Equal(t, "v2.0.0", ref)
	require.Equal(t, "3333333333333333333333333333333333333333", commit)
}

// A local run invents no source identity. An empty provenance source is
// honest; a fabricated one would be evidence of something that did not happen.
func TestProvenanceSource_LocallyInventsNothing(t *testing.T) {
	clearForgeEnv(t)

	repoURL, ref, commit := cienv.ProvenanceSource()

	require.Empty(t, repoURL, "a local run produced a repository URL")
	require.Empty(t, ref)
	require.Empty(t, commit)
	require.Empty(t, cienv.ProvenanceInvocationID(), "a local run produced an invocation URL")
}
