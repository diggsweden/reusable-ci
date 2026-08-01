// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

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

// TestBuildSBOMStatus_FailureBlocksRelease covers the failure-outcome
// path. SBOMs are mandatory, so a non-success outcome means the
// workflow has already failed before the status block ran (the report
// step uses `if: always()` to surface the failure in the summary even
// when the rest of the job has aborted).
func TestBuildSBOMStatus_FailureBlocksRelease(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.BuildSBOMStatus(context.Background(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "gradle-android", Outcome: "failure"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "release blocked") {
		t.Errorf("summary = %s", got)
	}
}
