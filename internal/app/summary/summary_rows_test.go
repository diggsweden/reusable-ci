// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// summaryRow names one Job Status row and where its result comes from: a
// stage and its wire target key, or a job input when stage is empty.
type summaryRow struct {
	label, stage, key string
}

// jobStatusTable extracts the Job Status table rows from a rendered summary.
func jobStatusTable(t *testing.T, body string) []string {
	t.Helper()

	_, rest, found := strings.Cut(body, "## Job Status\n| Job | Status |\n|-----|--------|\n")
	require.True(t, found, body)

	table, _, _ := strings.Cut(rest, "\n\n")

	return strings.Split(table, "\n")
}

// oneHotStages sets every row's source to success except failed, which fails.
func oneHotStages(t *testing.T, rows []summaryRow, failed int) (map[string]string, map[string]string) {
	t.Helper()

	stages, inputs := map[string]map[string]string{}, map[string]string{}

	for index, row := range rows {
		result := "success"
		if index == failed {
			result = "failure"
		}

		if row.stage == "" {
			inputs[row.key] = result

			continue
		}

		if stages[row.stage] == nil {
			stages[row.stage] = map[string]string{}
		}

		stages[row.stage][row.key] = result
	}

	encoded := map[string]string{}
	for stage, targets := range stages {
		encoded[stage] = stageResultJSON(t, stage, targets)
	}

	return encoded, inputs
}

func wantOneHot(rows []summaryRow, failed int) []string {
	want := make([]string, 0, len(rows))

	for index, row := range rows {
		icon := "✓"
		if index == failed {
			icon = "✗"
		}

		want = append(want, "| "+row.label+" | "+icon+" |")
	}

	return want
}

// TestReleaseSummary_EachRowShowsItsOwnResult fails one source at a time
// with every other row succeeding, and compares the whole Job Status table.
// The happy-path test sets a few rows and leaves Android, Xcode, Maven Central,
// Google Play and the promotions at their defaults, so a row reading its
// neighbour's target passed there; here any such swap puts the ✗ on the wrong
// row.
func TestReleaseSummary_EachRowShowsItsOwnResult(t *testing.T) {
	t.Parallel()

	rows := []summaryRow{
		{"Version Bump", "prepare", "version_bump"},
		{"Build Maven", "build", "maven"},
		{"Build NPM", "build", "npm"},
		{"Build Gradle", "build", "gradle"},
		{"Build Go", "build", "go"},
		{"Build Cargo", "build", "cargo"},
		{"Build Gradle Android", "build", "gradle_android"},
		{"Build Xcode", "build", "xcode_ios"},
		{"Publish Forge Packages", "publish", "forge_packages"},
		{"Publish Maven Central", "publish", "maven_central"},
		{"Publish Apple App Store", "publish", "xcode_ios"},
		{"Publish Google Play", "publish", "google_play"},
		{"Containers", "publish", "containers"},
		{"Cargo SBOM", "publish", "cargo_container_first"},
		{"Go SBOM", "publish", "go_container_first"},
		{"Promote Image → dev", "", "dev"},
		{"Promote Image → staging", "", "staging"},
		{"Promote Image → release", "", "release"},
		{"Forge Release", "", "forge"},
	}

	for failed := range rows {
		stages, inputs := oneHotStages(t, rows, failed)
		sink := &fakeSummarySink{}

		require.NoError(t, appsummary.ReleaseSummary(t.Context(), sink, appsummary.ReleaseSummaryInput{
			ReleaseVersion: "v1.2.3", PrepareStageJSON: stages["prepare"], BuildStageJSON: stages["build"], PublishStageJSON: stages["publish"],
			PromoteDevResult: inputs["dev"], PromoteStagingResult: inputs["staging"], PromoteReleaseResult: inputs["release"],
			CreateReleaseResult: inputs["forge"], Now: fixedNow(),
		}))
		require.Equal(t, wantOneHot(rows, failed), jobStatusTable(t, sink.buf.String()), rows[failed].label)
	}
}

// TestSnapshotReleaseSummary_EachRowShowsItsOwnResult does the same for the
// dev release rows of an npm project. The snapshot table has no container row
// on purpose: dev containers are reported by their own jobs, so a containers
// target must not add one.
func TestSnapshotReleaseSummary_EachRowShowsItsOwnResult(t *testing.T) {
	t.Parallel()

	rows := []summaryRow{
		{"Build Maven", "dev-build", "maven"},
		{"Build NPM", "dev-build", "npm"},
		{"Build Gradle", "dev-build", "gradle"},
		{"Build Go", "dev-build", "go"},
		{"Build Cargo", "dev-build", "cargo"},
		{"Build Gradle Android", "dev-build", "gradle_android"},
		{"Build Xcode", "dev-build", "xcode_ios"},
		{"Publish NPM Package", "dev-publish", "npm"},
		{"Cargo SBOM", "dev-publish", "cargo_container_first"},
		{"Go SBOM", "dev-publish", "go_container_first"},
		{"Dev SBOMs", "dev-publish", "sbom"},
	}

	// A succeeding containers target rides along in every run.
	sources := append(slices.Clone(rows), summaryRow{stage: "dev-publish", key: "containers"})

	for failed := range rows {
		stages, _ := oneHotStages(t, sources, failed)
		sink := &fakeSummarySink{}

		require.NoError(t, appsummary.SnapshotReleaseSummary(t.Context(), sink, io.Discard, appsummary.SnapshotReleaseSummaryInput{
			ProjectType: projecttype.NPM, ReleaseRef: "main", ReleaseSHA: "abcdef0123",
			BuildStageJSON: stages["dev-build"], PublishStageJSON: stages["dev-publish"], Now: fixedNow(),
		}))
		require.Equal(t, wantOneHot(rows, failed), jobStatusTable(t, sink.buf.String()), rows[failed].label)
	}
}

// TestSnapshotReleaseSummary_NPMPublicationStaysTruthful: metadata alone
// never claims a publication, and the already-exists note explains only a
// successful job. A failed job carrying the sentinel used to render
// "✗ (already published — skipped)".
func TestSnapshotReleaseSummary_NPMPublicationStaysTruthful(t *testing.T) {
	t.Parallel()

	const metadata = `{"npm_package_name":"my-pkg","npm_package_version":"0.0.0-dev.abc"%s}`

	for name, tc := range map[string]struct {
		status, sentinel string
		row, section     string
	}{
		"metadata with a failed publication": {
			status: "failure", row: "| Publish NPM Package | ✗ |", section: "### NPM Package\nNot published\n",
		},
		"already exists with a failed publication": {
			status: "failure", sentinel: `,"npm_publish_status":"already-exists"`, row: "| Publish NPM Package | ✗ |", section: "### NPM Package\nNot published\n",
		},
		"already exists with a skipped publication": {
			status: "skipped", sentinel: `,"npm_publish_status":"already-exists"`, row: "| Publish NPM Package | − |", section: "### NPM Package\nNot published\n",
		},
		"already exists with a successful publication": {
			status: "success", sentinel: `,"npm_publish_status":"already-exists"`,
			row:     "| Publish NPM Package | ✓ (already published — skipped) |",
			section: "### NPM Package\n> **Note:** Version already existed in registry — publish was skipped (same commit SHA).\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			require.NoError(t, appsummary.SnapshotReleaseSummary(t.Context(), sink, io.Discard, appsummary.SnapshotReleaseSummaryInput{
				ProjectType: projecttype.NPM, ReleaseRef: "main", ReleaseSHA: "abcdef0123",
				PublishStageJSON:      stageResultJSON(t, "dev-publish", map[string]string{"npm": tc.status}),
				SnapshotArtifactsJSON: strings.Replace(metadata, "%s", tc.sentinel, 1), Now: fixedNow(),
			}))

			body := sink.buf.String()
			require.Contains(t, jobStatusTable(t, body), tc.row)
			require.Contains(t, body, tc.section)

			if tc.status != "success" {
				require.NotContains(t, body, "already published")
				require.NotContains(t, body, "npm install")
			}
		})
	}
}

// TestPRSummary_LintRowNamesTheEngineThatRan covers the alternative engine:
// the single lint row is named after MegaLinter when it ran and Nanolinter was
// skipped, a cancelled engine still counts as having run, and the Swift row
// keeps its own result beside it.
func TestPRSummary_LintRowNamesTheEngineThatRan(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		targets map[string]string
		want    []string
	}{
		"megalinter failed": {
			targets: map[string]string{"nanolinter": "skipped", "megalinter": "failure", "swift": "success"},
			want:    []string{"| MegaLinter | ✗ |", "| Swift Lint | ✓ |"},
		},
		"megalinter passed, swift failed": {
			targets: map[string]string{"nanolinter": "skipped", "megalinter": "success", "swift": "failure"},
			want:    []string{"| MegaLinter | ✓ |", "| Swift Lint | ✗ |"},
		},
		"nanolinter cancelled": {
			targets: map[string]string{"nanolinter": "cancelled", "megalinter": "skipped", "swift": "skipped"},
			want:    []string{"| Nanolinter | ✗ |", "| Swift Lint | − |"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			require.NoError(t, appsummary.PRSummary(t.Context(), sink, appsummary.PRSummaryInput{
				QualityStageResultJSON: stageResultJSON(t, "pr-quality", tc.targets), Now: fixedNow(),
			}))

			_, table, found := strings.Cut(sink.buf.String(), "## Quality Check Status\n| Check | Status |\n|-------|--------|\n")
			require.True(t, found)

			rows, _, _ := strings.Cut(table, "\n\n")
			require.Equal(t, tc.want, strings.Split(rows, "\n"))
		})
	}
}
