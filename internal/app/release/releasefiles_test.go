// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestReleaseFiles_SelectsPublishesAndGuardsTheReleaseFileSet walks one release
// directory through collection, checksum-subject validation, manifest writing
// and section listing.
//
// The stages share a fixture and build on each other, so they stay in one test
// and run in order. They are subtests rather than a single run of statements
// because each is a separate claim: linear t.Fatal calls meant the first
// failure hid everything after it, and the name had grown into a list of the
// stages it covers.
//
// Note that ValidateReleaseChecksums does not hash anything. It decides which
// subjects a checksum file may name. Digest mismatch is VerifyDist's job and is
// covered in verifydist_test.go.
func TestReleaseFiles_SelectsPublishesAndGuardsTheReleaseFileSet(t *testing.T) {
	t.Chdir(t.TempDir())

	writeReleaseFilesFixture(t)

	assets, err := apprelease.CollectReleaseAssets(apprelease.FilesInput{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("collects every publishable artifact and its signature", func(t *testing.T) {
		for _, name := range []string{
			"app.tar.gz",
			"app.deb", "app.deb.sig",
			"app.rpm", "app.rpm.sig",
			"app.apk", "app.apk.sig",
			"app.tar.gz.sbom.json", "app.tar.gz.sbom.json.bundle",
			"app_checksums.txt", "app_checksums.txt.bundle",
			"slsa-provenance.intoto.json", "slsa-provenance.intoto.json.bundle",
		} {
			if !releaseFileEntryNamed(assets.Assets, name) {
				t.Errorf("expected release asset %q in %#v", name, releaseFileEntryNames(assets.Assets))
			}
		}
	})

	t.Run("leaves build internals and stray files out", func(t *testing.T) {
		for _, name := range []string{
			"metadata.json", "internal-binary",
			"image-sbom.cyclonedx.json", "image-sbom.cyclonedx.json.bundle",
			"trivy-results-core-amd64.json", "trivy-results-core-amd64.json.bundle",
			"doctor-core.json", "doctor-core.json.bundle",
			"stray.sbom.json", "stray.sbom.json.bundle",
		} {
			if releaseFileEntryNamed(assets.Assets, name) {
				t.Errorf("unexpected release asset %q", name)
			}
		}
	})

	t.Run("accepts a checksum file naming exactly the public assets", func(t *testing.T) {
		if validateErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "dist/app_checksums.txt"}); validateErr != nil {
			t.Fatal(validateErr)
		}
	})

	t.Run("rejects a checksum file listing a subject twice", func(t *testing.T) {
		writeReleaseFilesTestFile(t, "duplicate_checksums.txt", readFile(t, "dist/app_checksums.txt")+strings.Repeat("a", 64)+"  app.tar.gz\n")

		if dupErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "duplicate_checksums.txt"}); !errors.Is(dupErr, errs.ErrValidation) || !strings.Contains(dupErr.Error(), "duplicate subjects") {
			t.Fatalf("duplicate checksum err = %v", dupErr)
		}
	})

	t.Run("rejects a checksum file naming something never published", func(t *testing.T) {
		writeReleaseFilesTestFile(t, "nonpublic_checksums.txt", readFile(t, "dist/app_checksums.txt")+strings.Repeat("a", 64)+"  metadata.json\n")

		if nonpublicErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "nonpublic_checksums.txt"}); !errors.Is(nonpublicErr, errs.ErrValidation) || !strings.Contains(nonpublicErr.Error(), "not public release assets") {
			t.Fatalf("nonpublic checksum err = %v", nonpublicErr)
		}
	})

	withManifest := apprelease.FilesInput{ManifestFile: "dist/forgejo-ci-release-files.json"}

	t.Run("writes a manifest sorted into its sections", func(t *testing.T) {
		assetsJSON, marshalErr := json.Marshal(assets)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}

		writeReleaseFilesTestFile(t, "assets.json", string(assetsJSON))

		manifest, manifestErr := apprelease.WriteReleaseFileManifest(apprelease.WriteReleaseFileManifestInput{AssetsJSONFile: "assets.json", OutputFile: "dist/forgejo-ci-release-files.json"})
		if manifestErr != nil {
			t.Fatal(manifestErr)
		}

		if len(manifest.Checksums) != 1 || len(manifest.SBOMs) != 1 || len(manifest.Evidence) != 0 || len(manifest.Provenance) != 1 {
			t.Fatalf("manifest sections = checksums:%d sboms:%d evidence:%d provenance:%d", len(manifest.Checksums), len(manifest.SBOMs), len(manifest.Evidence), len(manifest.Provenance))
		}
	})

	t.Run("validates checksums against the written manifest", func(t *testing.T) {
		if manifestErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{FilesInput: withManifest, ChecksumsFile: "dist/app_checksums.txt"}); manifestErr != nil {
			t.Fatal(manifestErr)
		}
	})

	t.Run("lists a section from the manifest", func(t *testing.T) {
		sboms, listErr := apprelease.ListReleaseFiles(apprelease.ListReleaseFilesInput{FilesInput: withManifest, Section: "sboms"})
		if listErr != nil {
			t.Fatal(listErr)
		}

		if !slices.Equal(sboms, []string{"dist/app.tar.gz.sbom.json"}) {
			t.Fatalf("sboms = %v", sboms)
		}
	})
}

func TestReleaseFilesManifestRejectsImageLedger(t *testing.T) {
	t.Chdir(t.TempDir())

	writeReleaseFilesTestFile(t, "dist/release-images.json", "[]\n")

	manifest := apprelease.FileManifest{
		Version: appreleaseVersionForTest,
		Assets:  []apprelease.FileEntry{{Path: "dist/release-images.json", Name: "release-images.json", Source: "test"}},
		Checksums: []apprelease.FileEntry{{
			Path: "dist/release-images.json", Name: "release-images.json", Source: "checksum",
		}},
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	writeReleaseFilesTestFile(t, "dist/forgejo-ci-release-files.json", string(body))

	if err := apprelease.ValidateReleaseFileManifest(apprelease.FilesInput{ManifestFile: "dist/forgejo-ci-release-files.json"}); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "release image ledger") {
		t.Fatalf("err = %v", err)
	}
}

// appreleaseVersionForTest deliberately mirrors the public JSON contract, not an
// unexported implementation constant.
const appreleaseVersionForTest = 1

func writeReleaseFilesFixture(t *testing.T) {
	t.Helper()

	for _, file := range []string{
		"app.tar.gz",
		"app.deb", "app.deb.sig",
		"app.rpm", "app.rpm.sig",
		"app.apk", "app.apk.sig",
		"app.tar.gz.sbom.json", "app.tar.gz.sbom.json.bundle",
		"app_checksums.txt", "app_checksums.txt.bundle",
		"slsa-provenance.intoto.json", "slsa-provenance.intoto.json.bundle",
		"image-sbom.cyclonedx.json", "image-sbom.cyclonedx.json.bundle",
		"trivy-results-core-amd64.json", "trivy-results-core-amd64.json.bundle",
		"doctor-core.json", "doctor-core.json.bundle",
		"stray.sbom.json", "stray.sbom.json.bundle",
		"metadata.json", "internal-binary",
	} {
		writeReleaseFilesTestFile(t, filepath.Join("dist", file), file+"\n")
	}

	checksumSubjects := []string{"app.tar.gz", "app.deb", "app.rpm", "app.apk", "app.tar.gz.sbom.json"}

	checksumLines := make([]string, 0, len(checksumSubjects))
	for _, file := range checksumSubjects {
		checksumLines = append(checksumLines, strings.Repeat("a", 64)+"  "+file)
	}

	writeReleaseFilesTestFile(t, "dist/app_checksums.txt", strings.Join(checksumLines, "\n")+"\n")
	writeReleaseFilesTestFile(t, "dist/artifacts.json", `[
  {"path":"dist/app.tar.gz","type":"Archive"},
  {"path":"dist/app.deb","type":"LinuxPackage"},
  {"path":"dist/app.rpm","type":"LinuxPackage"},
  {"path":"dist/app.apk","type":"LinuxPackage"},
  {"path":"dist/app.tar.gz.sbom.json","type":"SBOM"},
  {"path":"dist/app_checksums.txt","type":"Checksum"},
  {"path":"dist/metadata.json","type":"Metadata"},
  {"path":"dist/internal-binary","type":"Binary"}
]`)
}

func releaseFileEntryNamed(entries []apprelease.FileEntry, name string) bool {
	return slices.Contains(releaseFileEntryNames(entries), name)
}

func releaseFileEntryNames(entries []apprelease.FileEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	return names
}

func writeReleaseFilesTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
