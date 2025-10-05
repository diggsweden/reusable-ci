// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestBuildSBOMStatus_AndroidGradleReportLayouts(t *testing.T) {
	t.Parallel()

	for _, report := range []string{"build/reports/bom.json", "build/reports/cyclonedx/bom.json", "app/build/reports/cyclonedx/bom.json"} {
		t.Run(report, func(t *testing.T) {
			t.Parallel()
			fsys := testfs.NewReal(t)
			path := fsys.WriteFile(report, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"type":"library","name":"owned-fixture","version":"1.0"}]}`))
			sink := &fakeSummarySink{}
			require.NoError(t, appsummary.BuildSBOMStatus(t.Context(), sink, appsummary.BuildSBOMStatusInput{
				Ecosystem: "gradle-android", Outcome: "success", WorkDir: fsys.Root,
			}))
			require.Equal(t, "### Build SBOM\n- \u2713 CycloneDX: `"+path+"`\n", sink.buf.String())
		})
	}
}

func TestBuildSBOMStatus_FindsMavenBOM(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("target/bom.json", []byte("{}"))

	sink := &fakeSummarySink{}

	if err := appsummary.BuildSBOMStatus(context.Background(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "maven", Outcome: "success", WorkDir: fsys.Root}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	got := sink.buf.String()
	if !strings.Contains(got, "CycloneDX (aggregate)") || !strings.Contains(got, "target/bom.json") {
		t.Errorf("summary = %s", got)
	}
}

func TestBuildSBOMStatus_ReportsMissingGradleBOM(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	sink := &fakeSummarySink{}

	if err := appsummary.BuildSBOMStatus(context.Background(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "gradle", Outcome: "success", WorkDir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "Generated but file not located") {
		t.Errorf("summary = %s", got)
	}
}

func TestBuildSBOMStatus_FailureReportsReleaseBlocked(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.BuildSBOMStatus(context.Background(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "gradle-android", Outcome: "failure"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "release blocked") || strings.Contains(got, "release continues") {
		t.Errorf("summary = %s", got)
	}
}

func TestBuildSBOMStatus_SkippedReportsDisabled(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.BuildSBOMStatus(context.Background(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "npm", Outcome: "skipped"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "Generation disabled") {
		t.Errorf("summary = %s", got)
	}
}
