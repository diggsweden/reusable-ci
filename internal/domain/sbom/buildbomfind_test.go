// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

func TestFindBuildBOM_PicksShallowestMatch(t *testing.T) {
	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files: []string{
			"release-artifacts/foo/sub/target/bom.json",
			"release-artifacts/foo/target/bom.json",
			"release-artifacts/target/bom.json",
		},
		Includes: []string{"*/target/bom.json"},
	})
	if got != "release-artifacts/target/bom.json" {
		t.Errorf("got %q", got)
	}
}

func TestFindBuildBOM_ExcludesNodeModules(t *testing.T) {
	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files: []string{
			"release-artifacts/node_modules/lib/bom.json",
			"release-artifacts/app/bom.json",
		},
		Includes: []string{"*/bom.json"},
		Excludes: []string{"*/node_modules/*"},
	})
	if got != "release-artifacts/app/bom.json" {
		t.Errorf("got %q (should skip node_modules)", got)
	}
}

func TestFindBuildBOM_MultipleIncludes(t *testing.T) {
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
	if got != "build/reports/bom.json" {
		t.Errorf("got %q", got)
	}
}

func TestFindBuildBOM_NoMatchReturnsEmpty(t *testing.T) {
	got := sbom.FindBuildBOM(sbom.FindBuildBOMInput{
		Files:    []string{"src/main.go"},
		Includes: []string{"*/bom.json"},
	})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
