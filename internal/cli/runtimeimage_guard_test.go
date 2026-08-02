// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
)

// TestRuntimeImageTagsShareOneVersion pins every reusable-ci-runtime-*
// image reference across the workflows to a single version tag. The
// runtime images are this repo's own release artifact — Renovate never
// bumps them — so a version cut rewrites every workflow default, and a
// missed site fails silently as a stale-runtime job rather than an
// error. This guard turns that silent drift into a failing build, in
// the same guardrail-test style as workflow-contract and docs-sync.
//
// The pattern definitions are shared with cmd/bump-runtime-tags (the
// tool that moves the pins on a release cut: `just bump-runtime-tags`)
// via internal/runtimetags, so rewriter and guard cannot drift apart.
func TestRuntimeImageTagsShareOneVersion(t *testing.T) {
	t.Parallel()

	// self-runtime-container.yml builds the runtime images themselves
	// and smoke-tests candidates under a local-only ":verify" tag that
	// never leaves the job. That tag is deliberate, not drift.
	verifyTagAllowedFiles := map[string]bool{
		".github/workflows/self-runtime-container.yml": true,
	}

	root := repoRoot(t)
	workflowsDir := filepath.Join(root, ".github", "workflows")

	entries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err)

	// tag → sorted, unique "file:line" sites referencing it.
	sites := map[string][]string{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		rel := ".github/workflows/" + entry.Name()

		content, readErr := os.ReadFile(filepath.Join(workflowsDir, entry.Name())) //nolint:gosec // test walks repo-local files.
		require.NoError(t, readErr)

		collectRuntimeTagSites(rel, string(content), verifyTagAllowedFiles, sites)
	}

	require.NotEmpty(t, sites, "no reusable-ci-runtime image references found under .github/workflows — pattern or layout changed?")

	tags := make([]string, 0, len(sites))
	for tag := range sites {
		tags = append(tags, tag)
	}

	sort.Strings(tags)

	if len(tags) > 1 {
		var report strings.Builder
		for _, tag := range tags {
			report.WriteString("\n  " + tag + ":\n    " + strings.Join(sites[tag], "\n    "))
		}

		require.Fail(t, "runtime image tags diverged",
			"every reusable-ci-runtime-* reference must carry the same version tag; a version cut must rewrite all of them together.%s", report.String())
	}

	require.Regexpf(t, runtimetags.VersionPattern, tags[0],
		"the shared runtime image tag %q is not a release version (vX.Y.Z); sites:\n  %s",
		tags[0], strings.Join(sites[tags[0]], "\n  "))
}

// collectRuntimeTagSites records every runtime-image tag in content as
// "rel:line" under its tag, skipping the local-only ":verify" tag in
// allowlisted files.
func collectRuntimeTagSites(rel, content string, verifyTagAllowedFiles map[string]bool, sites map[string][]string) {
	for i, line := range strings.Split(content, "\n") {
		for _, match := range runtimetags.RefPattern.FindAllStringSubmatch(line, -1) {
			tag := match[1]
			if tag == "verify" && verifyTagAllowedFiles[rel] {
				continue
			}

			sites[tag] = append(sites[tag], rel+":"+strconv.Itoa(i+1))
		}
	}
}

// TestRuntimeImageTagGuardCatchesDivergence proves the guard's scanner
// actually flags a drifted tag and honours the :verify allowlist, so a
// future regex or allowlist change cannot quietly blind the guard.
func TestRuntimeImageTagGuardCatchesDivergence(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{".github/workflows/self-runtime-container.yml": true}
	sites := map[string][]string{}

	collectRuntimeTagSites(".github/workflows/build-x.yml",
		`default: "ghcr.io/diggsweden/reusable-ci-runtime-base:v3.0.0"`, allowed, sites)
	collectRuntimeTagSites(".github/workflows/build-y.yml",
		`default: "ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.1.0"`, allowed, sites)
	collectRuntimeTagSites(".github/workflows/self-runtime-container.yml",
		`local-tag: reusable-ci-runtime-base:verify`, allowed, sites)
	collectRuntimeTagSites(".github/workflows/build-z.yml",
		`image: reusable-ci-runtime-node-24:verify`, allowed, sites)

	require.Len(t, sites, 3, "expected the two versions plus the non-allowlisted verify tag")
	require.Equal(t, []string{".github/workflows/build-x.yml:1"}, sites["v3.0.0"])
	require.Equal(t, []string{".github/workflows/build-y.yml:1"}, sites["v3.1.0"])
	require.Equal(t, []string{".github/workflows/build-z.yml:1"}, sites["verify"],
		"a :verify tag outside the allowlisted file must be flagged")
}
