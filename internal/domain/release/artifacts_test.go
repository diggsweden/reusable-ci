// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

func TestIsReleaseArtifact_MatchesKnownArchiveAndPackageExtensions(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"app.jar":                              true,
		"./release-artifacts/app.jar":          true,
		"frontend.tgz":                         true,
		"backend.tar.gz":                       true,
		"my-app.zip":                           true,
		"webapp.war":                           true,
		"original-app.jar":                     false,
		"./release-artifacts/original-foo.jar": false,
		"checksums.sha256":                     false, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"README.md":                            false,
		"app.jar.asc":                          false,
		"":                                     false,
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if got := release.IsReleaseArtifact(path); got != want {
				t.Errorf("IsReleaseArtifact(%q) = %v, want %v", path, got, want)
			}
		})
	}
}

func TestSBOMFilePatterns_HasExpectedShape(t *testing.T) {
	t.Parallel()

	// Order matters: callers glob in this sequence, so SPDX is discovered
	// before CycloneDX.
	want := []string{"*-sbom.spdx.json", "*-sbom.cyclonedx.json"}
	if !slices.Equal(release.SBOMFilePatterns, want) {
		t.Errorf("SBOMFilePatterns = %v, want %v", release.SBOMFilePatterns, want)
	}
}

func TestChecksumsFile_Constant(t *testing.T) {
	t.Parallel()

	if release.ChecksumsFile != "checksums.sha256" {
		t.Errorf("ChecksumsFile = %q", release.ChecksumsFile)
	}
}
