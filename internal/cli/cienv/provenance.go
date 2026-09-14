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

	formatted := time.Unix(secs, 0).UTC().Format(time.RFC3339)
	if _, err := time.Parse(time.RFC3339, formatted); err != nil {
		return "", false
	}

	return formatted, true
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
	value, _ := runcontext.ServerURL().ResolveAttested(runcontext.ProvenanceEnv(os.Getenv))

	return strings.TrimRight(value.String(), "/")
}

// ProvenanceSource returns the repository URL, short ref and commit SHA from
// the active runner's attested namespace, not the selected target provider.
// Missing values stay empty; in particular, local runs invent no source identity.
func ProvenanceSource() (string, string, string) {
	get := runcontext.ProvenanceEnv(os.Getenv)
	repository, _ := runcontext.Repository().ResolveAttested(get)
	ref, _ := runcontext.RefName().ResolveAttested(get)
	commit, _ := runcontext.Commit().ResolveAttested(get)
	// GitLab's repository/ref names are intentionally outside the shared
	// command flag chains; the filtered lookup admits them only on GitLab CI.
	server := envServerURL()
	repo := cmp.Or(repository.String(), get("CI_PROJECT_PATH"))

	var repoURL string
	if server != "" && repo != "" {
		repoURL = server + "/" + repo
	}

	return repoURL, cmp.Or(ref.String(), get("CI_COMMIT_REF_NAME")), commit.String()
}

// ProvenanceInvocationID returns the run/job URL identifying this CI
// invocation, forge-neutrally: the GitLab-native job URL when present, else
// the GitHub/Forgejo Actions run URL (Forgejo's native $FORGEJO_* names
// preferred over the $GITHUB_* aliases). Empty when nothing is resolvable.
func ProvenanceInvocationID() string {
	get := runcontext.ProvenanceEnv(os.Getenv)
	if jobURL := cmp.Or(get("CI_JOB_URL"), get("CI_PIPELINE_URL")); jobURL != "" {
		return jobURL
	}

	server := envServerURL()

	// The GitLab-native names stay OUTSIDE the shared chains on purpose:
	// commands get GitLab's dialect from the gitlab adapter's ResolveContext
	// (see EventName's doc). Provenance has no resolved context to fall back
	// on — it is a helper, not a flag — so it appends them here, after the
	// shared chain rather than instead of it.
	repository, _ := runcontext.Repository().ResolveAttested(get)
	run, _ := runcontext.RunID().ResolveAttested(get)
	repo, runID := repository.String(), run.String()

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
	get := runcontext.ProvenanceEnv(os.Getenv)
	if ref := cmp.Or(get("FORGEJO_WORKFLOW_REF"), get("GITHUB_WORKFLOW_REF")); ref != "" {
		if server := envServerURL(); server != "" {
			return server + "/" + ref
		}
	}

	return ProvenanceInvocationID()
}
