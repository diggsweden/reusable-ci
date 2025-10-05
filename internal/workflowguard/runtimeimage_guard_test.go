// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/runtimetags"
	"gopkg.in/yaml.v3"
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

	root := reporoot.Path(t)
	files, err := runtimetags.Files(root)
	require.NoError(t, err)

	// tag → sorted, unique "file:line" sites referencing it.
	sites := map[string][]string{}

	for _, rel := range files {
		content := reporoot.ReadFile(t, rel)
		collectRuntimeTagSites(filepath.ToSlash(rel), string(content), verifyTagAllowedFiles, sites)
	}

	require.NotEmpty(t, sites, "no reusable-ci-runtime image references found under .github/workflows — pattern or layout changed?")

	tags := make([]string, 0, len(sites))
	for tag := range sites {
		tags = append(tags, tag)
	}

	slices.Sort(tags)

	if len(tags) > 1 {
		var report strings.Builder
		for _, tag := range tags {
			report.WriteString("\n  " + tag + ":\n    " + strings.Join(sites[tag], "\n    "))
		}

		require.Fail(t, "runtime image tags diverged",
			"every reusable-ci-runtime-* reference must carry the same version tag; a version cut must rewrite all of them together.%s", report.String())
	}

	require.Truef(t, runtimetags.ValidVersion(tags[0]),
		"the shared runtime image tag %q is not a release version (vX.Y.Z); sites:\n  %s",
		tags[0], strings.Join(sites[tags[0]], "\n  "))
}

// collectRuntimeTagSites records every runtime-image tag in content as
// "rel:line" under its tag, skipping the local-only ":verify" tag in
// allowlisted files.
func collectRuntimeTagSites(rel, content string, verifyTagAllowedFiles map[string]bool, sites map[string][]string) {
	for i, line := range strings.Split(content, "\n") {
		for _, tag := range runtimetags.Tags(line) {
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

// binaryRefInput is the workflow input that names the reusable-ci revision a
// plain-runner job installs, as opposed to the runtime images that ship it
// pre-baked.
const binaryRefInput = "reusable-ci-binary-ref"

// TestReleaseRevisionIsOneModeledVersion couples the two halves of a release
// that TestRuntimeImageTagsShareOneVersion leaves independent. A job on a
// container runner gets the CLI baked into the runtime image; a job on a plain
// runner installs it from reusable-ci-binary-ref. Nothing made those two agree,
// so a version cut that rewrote the image pins and missed a binary-ref default
// would run one graph on two revisions: the container jobs on the new CLI, the
// plain-runner jobs on the old one, with the mismatch showing up as behaviour
// differences between jobs rather than as an error.
//
// Digest-pinned runtime defaults are refused for the same reason. A digest is
// a stronger pin than a tag, but it carries no version, so the shared-version
// check above cannot see it and the coupling here would have nothing to compare.
// Nothing pins one today; adding one deliberately means giving it a modeled
// version here rather than letting it slip past both guards.
func TestReleaseRevisionIsOneModeledVersion(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	files, err := runtimetags.Files(root)
	require.NoError(t, err)

	sites := map[string][]string{}
	binaryRefs := map[string][]string{}

	var digests []string

	for _, rel := range files {
		content := reporoot.ReadFile(t, rel)
		slash := filepath.ToSlash(rel)
		collectRuntimeTagSites(slash, string(content), map[string]bool{
			".github/workflows/self-runtime-container.yml": true,
		}, sites)
		collectReleaseRevisions(t, slash, content, binaryRefs, &digests)
	}

	require.Empty(t, digests,
		"a runtime image default pinned by digest carries no version, so neither the shared-tag guard nor this one can model it")
	require.Len(t, sites, 1, "runtime image tags diverged; TestRuntimeImageTagsShareOneVersion reports the sites")
	require.NotEmpty(t, binaryRefs, "no %s defaults found — input renamed or layout changed?", binaryRefInput)

	var runtimeTag string
	for tag := range sites {
		runtimeTag = tag
	}

	// The self graph builds and consumes the CLI of the release being cut before
	// that release exists, through one rolling pre-release tag. It is a second
	// modeled revision of the same version, not a second version.
	rolling := runtimeTag + rollingPreReleaseSuffix

	refs := make([]string, 0, len(binaryRefs))
	for ref := range binaryRefs {
		refs = append(refs, ref)
	}

	slices.Sort(refs)

	for _, ref := range refs {
		want := runtimeTag
		if rollingPreReleaseFiles(binaryRefs[ref]) {
			want = rolling
		}

		require.Equalf(t, want, ref,
			"the installed CLI revision must name the same release as the runtime images (%s); a version cut rewrites both together. Sites:\n  %s",
			runtimeTag, strings.Join(binaryRefs[ref], "\n  "))
	}

	for site, value := range rollingPreReleaseSites(t, files) {
		require.Equalf(t, rolling, value,
			"%s pins a rolling pre-release of another version; the pre-release tracks the release being cut", site)
	}
}

// rollingPreReleaseSuffix is the tag the self graph publishes and consumes for
// the release it is currently building.
const rollingPreReleaseSuffix = "-pre"

// rollingPreReleaseFiles reports whether every site of one binary-ref value is
// a workflow that deliberately tracks the rolling pre-release CLI. Only the
// operator-managed attestor does: it is invoked directly from an isolated
// repository rather than from this repo's release graph, and it consumes the
// pre-release assets the self graph publishes.
func rollingPreReleaseFiles(sites []string) bool {
	for _, site := range sites {
		file, _, _ := strings.Cut(site, ":")
		if file != ".github/workflows/slsa-attestor.yml" {
			return false
		}
	}

	return len(sites) > 0
}

// rollingPreReleaseSites finds every functional rolling pre-release literal in
// the runtime surface: input defaults, expression fallbacks and concurrency
// keys alike. Comment lines and input descriptions are prose, not revisions the
// run resolves, and are deliberately left alone.
func rollingPreReleaseSites(t *testing.T, files []string) map[string]string {
	t.Helper()

	found := map[string]string{}
	for _, rel := range files {
		collectRollingPreReleaseSites(filepath.ToSlash(rel), string(reporoot.ReadFile(t, rel)), found)
	}

	return found
}

func collectRollingPreReleaseSites(rel, content string, found map[string]string) {
	pattern := regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+` + rollingPreReleaseSuffix)

	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "description:") {
			continue
		}

		if match := pattern.FindString(line); match != "" {
			found[rel+":"+strconv.Itoa(index+1)] = match
		}
	}
}

// TestReleaseRevisionGuardCatchesDrift proves each half of the coupling fails
// when it should: a plain-runner job left on the previous CLI, a runtime image
// pinned by a digest that carries no version, and a rolling pre-release naming
// a release other than the one being cut. A guard that only ever passes against
// the current tree cannot show it would catch the next cut's missed site.
func TestReleaseRevisionGuardCatchesDrift(t *testing.T) {
	t.Parallel()

	refs, digests := map[string][]string{}, []string(nil)
	collectReleaseRevisions(t, ".github/workflows/stale.yml", []byte(`
on:
  workflow_call:
    inputs:
      reusable-ci-binary-ref:
        type: string
        default: "v2.9.0"
      base-image:
        type: string
        default: "ghcr.io/diggsweden/reusable-ci-runtime-base@sha256:`+strings.Repeat("a", 64)+`"
      unrelated:
        type: string
        default: "v3.0.0"
`), refs, &digests)

	require.Equal(t, map[string][]string{"v2.9.0": {".github/workflows/stale.yml:7"}}, refs,
		"only the binary-ref input names a CLI revision")
	require.Equal(t, []string{".github/workflows/stale.yml:10"}, digests,
		"a digest-pinned runtime default must be reported, not silently skipped")

	require.False(t, rollingPreReleaseFiles(refs["v2.9.0"]), "an ordinary workflow may not track the rolling pre-release")
	require.True(t, rollingPreReleaseFiles([]string{".github/workflows/slsa-attestor.yml:57"}))
	require.False(t, rollingPreReleaseFiles(nil), "no sites is not an exemption")
	require.False(t, rollingPreReleaseFiles([]string{".github/workflows/slsa-attestor.yml:57", ".github/workflows/other.yml:12"}),
		"one exempt site does not exempt the value everywhere it appears")

	sites := map[string]string{}
	collectRollingPreReleaseSites(".github/workflows/self.yml", strings.Join([]string{
		`      version-tag: ${{ github.ref_type == 'tag' && github.ref_name || 'v2.9.0-pre' }}`,
		`      group: self-runtime-cli-v3.0.0-pre`,
		`        description: "for example v1.0.0-pre"`,
		`      # a comment mentioning v0.1.0-pre`,
		`      unrelated: "v3.0.0"`,
	}, "\n"), sites)
	require.Equal(t, map[string]string{
		".github/workflows/self.yml:1": "v2.9.0-pre",
		".github/workflows/self.yml:2": "v3.0.0-pre",
	}, sites, "expression fallbacks and concurrency keys count; prose does not")
}

// collectReleaseRevisions records every reusable-ci-binary-ref default as
// "rel:line" under its value, and every digest-pinned runtime image default.
func collectReleaseRevisions(t *testing.T, rel string, content []byte, refs map[string][]string, digests *[]string) {
	t.Helper()

	// Parsed from the source rather than through effectiveWorkflow: that
	// round-trips the document, and a site is only actionable with the line the
	// author has to edit.
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil || len(doc.Content) == 0 {
		return // Not every runtime surface file is a workflow document.
	}

	inputs := mappingChild(mappingChild(mappingChild(doc.Content[0], "on"), "workflow_call"), "inputs")
	if inputs == nil || inputs.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i+1 < len(inputs.Content); i += 2 {
		value := mappingChild(inputs.Content[i+1], "default")
		if value == nil || value.Kind != yaml.ScalarNode {
			continue
		}

		site := rel + ":" + strconv.Itoa(value.Line)
		if inputs.Content[i].Value == binaryRefInput {
			refs[value.Value] = append(refs[value.Value], site)

			continue
		}

		if strings.Contains(value.Value, "reusable-ci-runtime") && strings.Contains(value.Value, "@sha256:") {
			*digests = append(*digests, site)
		}
	}
}
