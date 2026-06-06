// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestSBOMCountStatus_CountsGoBOMs(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile(".reusable-ci/go-build-sbom/app/bom.json", []byte("{}"))
	fsys.WriteFile(".reusable-ci/go-build-sbom/cli/bom.json", []byte("{}"))

	sink := &fakeSummarySink{}

	if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: "go", Outcome: "success", WorkDir: fsys.Root}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "2 bom.json") {
		t.Errorf("summary = %s", got)
	}
}

func TestSBOMCountStatus_CargoExcludesTarget(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	fsys.WriteFile("bom.json", []byte("{}"))
	fsys.WriteFile("target/bom.json", []byte("{}"))

	sink := &fakeSummarySink{}

	if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: "cargo", Outcome: "success", WorkDir: fsys.Root}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "1 bom.json") {
		t.Errorf("summary = %s", got)
	}
}

// TestSBOMCountStatus_FailureBlocksRelease — sibling of
// TestBuildSBOMStatus_FailureBlocksRelease. SBOMs are mandatory, so a
// non-success outcome means the workflow has already failed before this
// status block runs under `if: always()`.
func TestSBOMCountStatus_FailureBlocksRelease(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: "cargo", Outcome: "failure"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "release blocked") {
		t.Errorf("summary = %s", got)
	}
}
