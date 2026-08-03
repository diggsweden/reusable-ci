// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv

import (
	"cmp"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
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
// trimmed.
//
// These helpers resolve a VALUE rather than declare a flag, which is why
// they once hand-rolled their own cmp.Or(os.Getenv(...)) chains — and why
// they drifted from the chains in this very package: this one had lost
// $FORGEJO_SERVER. Resolving the shared runcontext.Var against os.Getenv
// gives the value without a second name list.
func envServerURL() string {
	return strings.TrimSuffix(runcontext.ServerURL().Resolve(os.Getenv), "/")
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

	// The GitLab-native names stay OUTSIDE the shared chains on purpose:
	// commands get GitLab's dialect from the gitlab adapter's ResolveContext
	// (see EventName's doc). Provenance has no resolved context to fall back
	// on — it is a helper, not a flag — so it appends them here, after the
	// shared chain rather than instead of it.
	repo := cmp.Or(runcontext.Repository().Resolve(os.Getenv), os.Getenv("CI_PROJECT_PATH"))
	runID := cmp.Or(runcontext.RunID().Resolve(os.Getenv), os.Getenv("CI_PIPELINE_ID"))

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
