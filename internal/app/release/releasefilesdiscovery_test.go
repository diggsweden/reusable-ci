// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ListReleaseFiles has two halves: read a written manifest, or discover
// from disk when none exists. Only the manifest half was tested, and the
// discovery half is the one a release takes on a first run or when the
// manifest step was skipped -- so it is what decides which files are
// published.
//
// These tests use t.Chdir, so no t.Parallel().

// listSection is ListReleaseFiles against the fixture's dist dir with no
// manifest present, which is what forces the discovery path.
func listSection(t *testing.T, section string) ([]string, error) {
	t.Helper()

	return apprelease.ListReleaseFiles(apprelease.ListReleaseFilesInput{
		FilesInput: apprelease.FilesInput{ManifestFile: filepath.Join(t.TempDir(), "absent.json")},
		Section:    section,
	})
}

func TestListReleaseFiles_DiscoversEachSectionFromDisk(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	t.Run("assets", func(t *testing.T) {
		got, err := listSection(t, "assets")
		if err != nil {
			t.Fatal(err)
		}

		for _, want := range []string{"dist/app.tar.gz", "dist/app.deb", "dist/app_checksums.txt"} {
			if !slices.Contains(got, want) {
				t.Errorf("%s missing from the discovered assets: %v", want, got)
			}
		}

		// A raw binary is a build internal. Publishing it alongside the
		// archives ships an unsigned, unchecksummed file under a name
		// nothing else references.
		if slices.Contains(got, "dist/internal-binary") {
			t.Error("the raw binary was discovered as a publishable asset")
		}
	})

	t.Run("checksums", func(t *testing.T) {
		got, err := listSection(t, "checksums")
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(got, []string{"dist/app_checksums.txt"}) {
			t.Errorf("checksums = %v, want exactly the one checksums file", got)
		}
	})

	t.Run("sboms", func(t *testing.T) {
		got, err := listSection(t, "sboms")
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Contains(got, "dist/app.tar.gz.sbom.json") {
			t.Errorf("the dist SBOM was not discovered: %v", got)
		}

		// The image SBOM and a stray .sbom.json are not GoReleaser
		// artifacts; discovery reads artifacts.json, not the directory.
		for _, unwanted := range []string{"dist/image-sbom.cyclonedx.json", "dist/stray.sbom.json"} {
			if slices.Contains(got, unwanted) {
				t.Errorf("%s was discovered as a dist SBOM: %v", unwanted, got)
			}
		}
	})

	t.Run("provenance", func(t *testing.T) {
		got, err := listSection(t, "provenance")
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(got, []string{"dist/slsa-provenance.intoto.json"}) {
			t.Errorf("provenance = %v, want the envelope", got)
		}
	})
}

// TestListReleaseFiles_SectionNames covers the two ends of the section
// switch: the one that is deliberately empty, and the one that must not
// be. A typo'd section in a workflow would otherwise publish nothing and
// report success.
func TestListReleaseFiles_SectionNames(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	got, err := listSection(t, "evidence")
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("evidence = %v, want none", got)
	}

	if _, err := listSection(t, "signatures"); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage naming the valid sections", err)
	}
}

// TestListReleaseFiles_ABinaryIsNeverAPublishableAsset pins that the
// GoReleaser artifact TYPE governs, not the filename. A binary is a
// build internal -- publishing one ships an unsigned, unchecksummed file
// under a name nothing else references -- and the suffix list alone
// would let one through as soon as it were named like an archive.
func TestListReleaseFiles_ABinaryIsNeverAPublishableAsset(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	// A Binary artifact whose path carries a publishable suffix: only
	// the type check can exclude this one.
	writeReleaseFilesTestFile(t, "dist/tool.zip", "tool\n")
	writeReleaseFilesTestFile(t, "dist/artifacts.json", `[
  {"path":"dist/app.tar.gz","type":"Archive"},
  {"path":"dist/app_checksums.txt","type":"Checksum"},
  {"path":"dist/tool.zip","type":"Binary"}
]`)

	got, err := listSection(t, "assets")
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(got, "dist/tool.zip") {
		t.Errorf("a Binary artifact was published because its name looked like an archive: %v", got)
	}

	if !slices.Contains(got, "dist/app.tar.gz") {
		t.Errorf("the real archive was dropped: %v", got)
	}
}

// TestListReleaseFiles_ChecksumDiscoveryRefusesAmbiguity covers the count
// check. The checksums file is what the signer signs and what verifiers
// fetch, so "whichever one sorted first" is not an answer -- and neither
// is silence when there is none.
func TestListReleaseFiles_ChecksumDiscoveryRefusesAmbiguity(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	// A second one, as a nested build directory would produce.
	writeReleaseFilesTestFile(t, "dist/nested/other_checksums.txt", "deadbeef  app.tar.gz\n")

	_, err := listSection(t, "checksums")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for two checksum files", err)
	}

	if !strings.Contains(err.Error(), "found 2") {
		t.Errorf("the error does not say how many were found: %v", err)
	}
}

func TestListReleaseFiles_ChecksumDiscoveryRefusesAbsence(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.MkdirAll("dist", 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := listSection(t, "checksums")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation when no checksums file exists", err)
	}
}

// TestListReleaseFiles_ChecksumDiscoverySkipsSymlinks covers the guard in
// the walk. A symlink named checksums.txt is not a build output; treating
// one as the release's checksum file would publish, and sign, whatever it
// points at.
func TestListReleaseFiles_ChecksumDiscoverySkipsSymlinks(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	outside := filepath.Join(t.TempDir(), "elsewhere_checksums.txt")
	if err := os.WriteFile(outside, []byte("deadbeef  passwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, "dist/linked_checksums.txt"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// The symlink is skipped, so the real file is still unambiguous.
	got, err := listSection(t, "checksums")
	if err != nil {
		t.Fatalf("a symlink was counted as a second checksums file: %v", err)
	}

	if !slices.Equal(got, []string{"dist/app_checksums.txt"}) {
		t.Errorf("checksums = %v, want only the real file", got)
	}
}

// TestListReleaseFiles_ProvenanceSymlinkIsNotPublished is the same guard
// on the provenance envelope: the signer's own statement about the build
// must be a file the build wrote.
func TestListReleaseFiles_ProvenanceSymlinkIsNotPublished(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesFixture(t)

	if err := os.Remove("dist/slsa-provenance.intoto.json"); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "someone-elses-provenance.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, "dist/slsa-provenance.intoto.json"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := listSection(t, "provenance")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Errorf("a symlinked provenance envelope was published: %v", got)
	}
}
