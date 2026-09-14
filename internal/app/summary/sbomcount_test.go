// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const (
	sbomFailedLine   = "- ⚠️ Generation failed; release blocked until the Build SBOM succeeds or is explicitly disabled\n"
	sbomDisabledLine = "- ⊘ Generation disabled; release continues without a build SBOM\n"
)

// TestSBOMCountStatus_ReportsEachOutcomeExactly pins the whole block for every
// outcome, with an empty tree so success reaches the zero-count message. The
// workflow passes a step outcome, which can also be cancelled; anything but
// success or skipped reports the failure wording rather than claiming the
// release continues.
func TestSBOMCountStatus_ReportsEachOutcomeExactly(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		kind, outcome, want string
	}{
		"go success without files":    {kind: "go", outcome: "success", want: "### Go Build SBOM\n- ⚠️ cyclonedx-gomod reported success but produced no bom.json\n"},
		"cargo success without files": {kind: "cargo", outcome: "success", want: "### Build SBOM\n- ⚠️ cargo-cyclonedx reported success but produced no bom.json\n"},
		"skipped":                     {kind: "go", outcome: "skipped", want: "### Go Build SBOM\n" + sbomDisabledLine},
		"failure":                     {kind: "cargo", outcome: "failure", want: "### Build SBOM\n" + sbomFailedLine},
		"cancelled":                   {kind: "cargo", outcome: "cancelled", want: "### Build SBOM\n" + sbomFailedLine},
		"no outcome":                  {kind: "go", outcome: "", want: "### Go Build SBOM\n" + sbomFailedLine},
		"kind with surrounding space": {kind: " go ", outcome: "skipped", want: "### Go Build SBOM\n" + sbomDisabledLine},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}

			err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{
				Kind: tc.kind, Outcome: tc.outcome, WorkDir: t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}

			if got := sink.buf.String(); got != tc.want {
				t.Errorf("summary =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestSBOMCountStatus_UnknownKindAppendsNothing(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"", "npm", "Go", "go-mod"} {
		sink := &fakeSummarySink{}

		err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: kind, Outcome: "success"})
		if !errors.Is(err, errs.ErrUsage) || sink.buf.Len() != 0 {
			t.Errorf("kind %q: err = %v, summary = %q; want ErrUsage and nothing appended", kind, err, sink.buf.String())
		}
	}
}

// TestSBOMCountStatus_CountsOnlyTheRelevantTree covers where each kind looks.
// Go counts only its own output directory, so a bom.json elsewhere in the
// checkout is not one of its SBOMs. Cargo counts the whole tree except build
// output in a target directory at any depth -- but not the walk root itself,
// so a crate checked out into a directory called target still counts.
func TestSBOMCountStatus_CountsOnlyTheRelevantTree(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		kind  string
		files []string
		sub   string
		want  string
	}{
		"go": {
			kind:  "go",
			files: []string{"bom.json", ".reusable-ci/go-build-sbom/app/bom.json", ".reusable-ci/go-build-sbom/cli/bom.json", ".reusable-ci/other/bom.json"},
			want:  "### Go Build SBOM\n- ✓ CycloneDX: 2 bom.json file(s)\n",
		},
		"cargo": {
			kind:  "cargo",
			files: []string{"bom.json", "crates/a/bom.json", "target/bom.json", "crates/a/target/debug/bom.json", "crates/a/bom.json.bak"},
			want:  "### Build SBOM\n- ✓ CycloneDX: 2 bom.json file(s)\n",
		},
		"cargo rooted in a directory named target": {
			kind:  "cargo",
			files: []string{"target/bom.json", "target/target/bom.json"},
			sub:   "target",
			want:  "### Build SBOM\n- ✓ CycloneDX: 1 bom.json file(s)\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			for _, file := range tc.files {
				fsys.WriteFile(file, []byte("{}"))
			}

			sink := &fakeSummarySink{}

			if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{
				Kind: tc.kind, Outcome: "success", WorkDir: filepath.Join(fsys.Root, tc.sub),
			}); err != nil {
				t.Fatal(err)
			}

			if got := sink.buf.String(); got != tc.want {
				t.Errorf("summary =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestSBOMCountStatus_CountsWhatItCanRead pins the partial-count policy: an
// unreadable directory is left out of the count instead of failing the
// summary, which runs with if: always() after the step it describes.
func TestSBOMCountStatus_CountsWhatItCanRead(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}

	fsys := testfs.NewReal(t)
	fsys.WriteFile("crates/open/bom.json", []byte("{}"))
	locked := filepath.Dir(fsys.WriteFile("crates/locked/bom.json", []byte("{}")))

	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:gosec // a directory needs its search bit; restores access so the temp dir can be removed.

	sink := &fakeSummarySink{}

	if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: "cargo", Outcome: "success", WorkDir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); got != "### Build SBOM\n- ✓ CycloneDX: 1 bom.json file(s)\n" {
		t.Errorf("summary = %q, want the readable bom.json counted", got)
	}
}

// TestSBOMCountStatus_DefaultsToTheWorkingDirectory covers an omitted WorkDir.
func TestSBOMCountStatus_DefaultsToTheWorkingDirectory(t *testing.T) {
	// Not parallel: t.Chdir.
	fsys := testfs.NewReal(t)
	fsys.WriteFile(".reusable-ci/go-build-sbom/app/bom.json", []byte("{}"))
	t.Chdir(fsys.Root)

	sink := &fakeSummarySink{}

	if err := appsummary.SBOMCountStatus(context.Background(), sink, appsummary.SBOMCountStatusInput{Kind: "go", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); got != "### Go Build SBOM\n- ✓ CycloneDX: 1 bom.json file(s)\n" {
		t.Errorf("summary = %q", got)
	}
}
