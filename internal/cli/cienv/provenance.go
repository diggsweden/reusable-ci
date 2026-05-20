// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv

import (
	"cmp"
	"os"
	"strconv"
	"strings"
	"time"
)

// SourceDateEpochRFC3339 returns $SOURCE_DATE_EPOCH (unix seconds, the
// repo-wide reproducible-build timestamp) formatted as RFC3339 UTC, and ok=true
// when it is set and parses. ok=false (and "") when unset or malformed — so
// optional callers (container provenance) can fall back gracefully while
// required callers (release provenance) can turn ok=false into an error.
func SourceDateEpochRFC3339() (string, bool) {
	epoch := os.Getenv("SOURCE_DATE_EPOCH")
	if epoch == "" {
		return "", false
	}

	secs, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return "", false
	}

	return time.Unix(secs, 0).UTC().Format(time.RFC3339), true
}

// envServerURL reads the forge server URL forge-neutrally, trailing slash
// trimmed: CI_SERVER_URL (GitLab/neutral) → Forgejo's native
// FORGEJO_SERVER_URL → the GITHUB_SERVER_URL compat alias. Forgejo's own
// name comes before the alias (matching ServerURL()), since Forgejo Runner
// 7.0.0 lets workflows drop the GITHUB_ names.
func envServerURL() string {
	return strings.TrimSuffix(
		cmp.Or(os.Getenv("CI_SERVER_URL"), os.Getenv("FORGEJO_SERVER_URL"), os.Getenv("GITHUB_SERVER_URL")),
		"/",
	)
}

// ProvenanceInvocationID returns the run/job URL identifying this CI
// invocation, forge-neutrally: the GitLab-native job URL when present, else
// the GitHub/Forgejo Actions run URL (Forgejo's native $FORGEJO_* names
// preferred over the $GITHUB_* aliases). Empty when nothing is resolvable.
func ProvenanceInvocationID() string {
	if jobURL := os.Getenv("CI_JOB_URL"); jobURL != "" {
		return jobURL
	}

	server := envServerURL()
	repo := cmp.Or(os.Getenv("FORGEJO_REPOSITORY"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("CI_PROJECT_PATH"))
	runID := cmp.Or(os.Getenv("FORGEJO_RUN_ID"), os.Getenv("GITHUB_RUN_ID"), os.Getenv("CI_PIPELINE_ID"))

	if server != "" && repo != "" && runID != "" {
		return server + "/" + repo + "/actions/runs/" + runID
	}

	return ""
}

// ProvenanceBuilderID derives a forge-neutral SLSA builder identity from the
// CI env: the CANONICAL workflow ref (<server>/<GITHUB_WORKFLOW_REF>, e.g.
// https://github.com/o/r/.github/workflows/x.yml@refs/tags/v1) when available,
// else the invocation URL. Shared by every provenance generator (container
// attest, release provenance) so the builder identity is identical across
// artifact types — never the human-facing workflow display name.
func ProvenanceBuilderID() string {
	if ref := cmp.Or(os.Getenv("FORGEJO_WORKFLOW_REF"), os.Getenv("GITHUB_WORKFLOW_REF")); ref != "" {
		if server := envServerURL(); server != "" {
			return server + "/" + ref
		}
	}

	return ProvenanceInvocationID()
}
