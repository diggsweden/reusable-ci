// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestIsReleaseArtifact(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"app.jar":                     true,
		"./release-artifacts/app.jar": true,
		// An Android library's AAR is a release asset like any other jar:
		// it is attached to the release and GPG-signed alongside it.
		"mylib-release.aar":                    true,
		"./release-artifacts/lib-release.aar":  true,
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

	want := []string{"*-sbom.spdx.json", "*-sbom.cyclonedx.json"}
	if len(release.SBOMFilePatterns) != len(want) {
		t.Fatalf("got %d patterns, want %d", len(release.SBOMFilePatterns), len(want))
	}

	for i, p := range release.SBOMFilePatterns {
		if p != want[i] {
			t.Errorf("pattern[%d] = %q, want %q", i, p, want[i])
		}
	}
}

func TestChecksumsFile_Constant(t *testing.T) {
	t.Parallel()

	if release.ChecksumsFile != "checksums.sha256" {
		t.Errorf("ChecksumsFile = %q", release.ChecksumsFile)
	}
}
