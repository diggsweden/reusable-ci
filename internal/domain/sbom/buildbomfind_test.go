// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
)

func TestFindBuildBOM_PicksShallowestMatch(t *testing.T) {
	t.Parallel()

	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files: []string{
			"release-artifacts/foo/sub/target/bom.json",
			"release-artifacts/foo/target/bom.json",
			"release-artifacts/target/bom.json",
		},
		Includes: []string{"*/target/bom.json"},
	})
	if want := "release-artifacts/target/bom.json"; got != want {
		t.Errorf("got %q, want %q (the shallowest match)", got, want)
	}
}

func TestFindBuildBOM_ExcludesNodeModules(t *testing.T) {
	t.Parallel()

	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files: []string{
			"release-artifacts/node_modules/lib/bom.json",
			"release-artifacts/app/bom.json",
		},
		Includes: []string{"*/bom.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Excludes: []string{"*/node_modules/*"},
	})
	if got != "release-artifacts/app/bom.json" {
		t.Errorf("got %q (should skip node_modules)", got)
	}
}

func TestFindBuildBOM_MultipleIncludes(t *testing.T) {
	t.Parallel()

	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files: []string{
			"build/reports/cyclonedx/bom.json",
			"build/reports/bom.json",
		},
		Includes: []string{
			"*/build/reports/bom.json",
			"*/build/reports/cyclonedx/bom.json",
		},
	})
	// Both match — shallower wins.
	if want := "build/reports/bom.json"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindBuildBOM_NoMatchReturnsEmpty(t *testing.T) {
	t.Parallel()

	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files:    []string{"src/main.go"},
		Includes: []string{"*/bom.json"},
	})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestFindBuildBOM_ExcludesBeatDepthAndTiesAreLexical covers the two rules
// the shallowest-match test cannot: an excluded candidate is dropped even
// when it is the shallowest (the Go pattern excludes */dist/*, where a copied
// BOM sits above the real one), and equal-depth candidates are chosen by
// path order, not file order.
func TestFindBuildBOM_ExcludesBeatDepthAndTiesAreLexical(t *testing.T) {
	t.Parallel()

	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files:    []string{"release-artifacts/dist/bom.json", "release-artifacts/app/target/bom.json"},
		Includes: []string{"*/bom.json"},
		Excludes: []string{"*/dist/*"},
	})
	if got != "release-artifacts/app/target/bom.json" {
		t.Errorf("got %q: the shallower excluded candidate was kept", got)
	}

	got = sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files:    []string{"release-artifacts/b/bom.json", "release-artifacts/a/bom.json"},
		Includes: []string{"*/bom.json"},
	})
	if got != "release-artifacts/a/bom.json" {
		t.Errorf("got %q: equal-depth candidates must break ties by path", got)
	}
}
