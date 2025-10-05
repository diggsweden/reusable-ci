// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
// ValidateReleaseChecksums enforces both the public subject set and each
// subject's actual file digest. VerifyDist separately protects the complete tree
// while it crosses a job boundary.
func TestReleaseFiles_SelectsPublishesAndGuardsTheReleaseFileSet(t *testing.T) {
	t.Chdir(t.TempDir())

	writeReleaseFilesFixture(t)

	assets, err := apprelease.CollectReleaseAssets(apprelease.FilesInput{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("collects every publishable artifact and its signature", func(t *testing.T) {
		assertReleaseAssetsInclude(t, assets.Assets,
			"app.tar.gz",
			"app.deb", "app.deb.sig",
			"app.rpm", "app.rpm.sig",
			"app.apk", "app.apk.sig",
			"app.tar.gz.sbom.json", "app.tar.gz.sbom.json.bundle",
			"app_checksums.txt", "app_checksums.txt.bundle",
			"slsa-provenance.intoto.json", "slsa-provenance.intoto.json.bundle",
		)
	})

	t.Run("leaves build internals and stray files out", func(t *testing.T) {
		assertReleaseAssetsExclude(t, assets.Assets,
			"metadata.json", "internal-binary",
			"image-sbom.cyclonedx.json", "image-sbom.cyclonedx.json.bundle",
			"trivy-results-core-amd64.json", "trivy-results-core-amd64.json.bundle",
			"doctor-core.json", "doctor-core.json.bundle",
			"stray.sbom.json", "stray.sbom.json.bundle",
		)
	})

	t.Run("accepts a checksum file naming exactly the public assets", func(t *testing.T) {
		if validateErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "dist/app_checksums.txt"}); validateErr != nil {
			t.Fatal(validateErr)
		}
	})

	t.Run("rejects a checksum digest that does not match the release file", func(t *testing.T) {
		body := readFile(t, "dist/app_checksums.txt")
		writeReleaseFilesTestFile(t, "digest-mismatch_checksums.txt", strings.Repeat("0", 64)+body[64:])

		validateErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "digest-mismatch_checksums.txt"})
		if !errors.Is(validateErr, errs.ErrValidation) || !strings.Contains(validateErr.Error(), "checksum digest mismatch for app.tar.gz") {
			t.Fatalf("err = %v, want app.tar.gz digest mismatch", validateErr)
		}
	})

	t.Run("rejects a checksum file listing a subject twice", func(t *testing.T) {
		requireChecksumFileRejected(t, "duplicate_checksums.txt", "app.tar.gz", "duplicate subjects")
	})

	t.Run("rejects a checksum file naming something never published", func(t *testing.T) {
		requireChecksumFileRejected(t, "nonpublic_checksums.txt", "metadata.json", "not public release assets")
	})

	t.Run("rejects a checksum file missing a public release asset", func(t *testing.T) {
		lines := strings.Split(readFile(t, "dist/app_checksums.txt"), "\n")
		writeReleaseFilesTestFile(t, "missing_checksums.txt", strings.Join(lines[1:], "\n"))

		validateErr := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "missing_checksums.txt"})
		if !errors.Is(validateErr, errs.ErrValidation) || !strings.Contains(validateErr.Error(), "missing public release assets") {
			t.Fatalf("err = %v, want missing public release asset", validateErr)
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

func TestValidateReleaseChecksums_HashesNestedManifestPath(t *testing.T) {
	t.Chdir(t.TempDir())

	const (
		artifactPath = "dist/nested/app.tar.gz"
		checksumPath = "dist/app_checksums.txt"
		manifestPath = "dist/release-files.json"
	)

	writeReleaseFilesTestFile(t, artifactPath, "release archive\n")
	writeReleaseFilesTestFile(t, checksumPath, testFileSHA256(t, artifactPath)+"  app.tar.gz\n")

	manifest := apprelease.FileManifest{
		Version: appreleaseVersionForTest,
		Assets: []apprelease.FileEntry{
			{Path: artifactPath, Name: "app.tar.gz", Source: "test"},
			{Path: checksumPath, Name: "app_checksums.txt", Source: "test"},
		},
		Checksums: []apprelease.FileEntry{{Path: checksumPath, Name: "app_checksums.txt", Source: "checksum"}},
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	writeReleaseFilesTestFile(t, manifestPath, string(body))

	input := apprelease.ValidateReleaseChecksumsInput{
		FilesInput:    apprelease.FilesInput{DistDir: "dist", ManifestFile: manifestPath},
		ChecksumsFile: checksumPath,
	}
	if err := apprelease.ValidateReleaseChecksums(input); err != nil {
		t.Fatalf("validate nested manifest path: %v", err)
	}

	writeReleaseFilesTestFile(t, artifactPath, "changed archive\n")

	if err := apprelease.ValidateReleaseChecksums(input); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "checksum digest mismatch for app.tar.gz") {
		t.Fatalf("err = %v, want nested artifact digest mismatch", err)
	}
}

func TestValidateReleaseChecksums_RejectsAmbiguousArtifactBasenames(t *testing.T) {
	t.Chdir(t.TempDir())

	const firstPath = "dist/first/app.tar.gz"
	writeReleaseFilesTestFile(t, firstPath, "first archive\n")
	writeReleaseFilesTestFile(t, "dist/second/app.tar.gz", "second archive\n")
	writeReleaseFilesTestFile(t, "dist/app_checksums.txt", testFileSHA256(t, firstPath)+"  app.tar.gz\n")
	writeReleaseFilesTestFile(t, "dist/artifacts.json", `[
  {"path":"dist/first/app.tar.gz","type":"Archive"},
  {"path":"dist/second/app.tar.gz","type":"Archive"},
  {"path":"dist/app_checksums.txt","type":"Checksum"}
]`)

	err := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: "dist/app_checksums.txt"})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "duplicate release asset basename: app.tar.gz") {
		t.Fatalf("err = %v, want duplicate nested artifact basename", err)
	}
}

func TestWriteReleaseFileManifest_PreservesExplicitEvidence(t *testing.T) {
	t.Chdir(t.TempDir())

	for path, body := range map[string]string{
		"dist/app.tar.gz":                         "release archive\n",
		"dist/checksums.sha256":                   "",
		"dist/checksums.sha256.bundle":            "checksum bundle\n",
		"dist/doctor.json":                        "custom evidence\n",
		"dist/doctor.json.bundle":                 "evidence bundle\n",
		"dist/slsa-provenance.intoto.json":        "provenance\n",
		"dist/slsa-provenance.intoto.json.bundle": "provenance bundle\n",
	} {
		writeReleaseFilesTestFile(t, path, body)
	}

	writeReleaseFilesTestFile(t, "dist/checksums.sha256", testFileSHA256(t, "dist/app.tar.gz")+"  app.tar.gz\n")
	writeReleaseFilesTestFile(t, "dist/artifacts.json", `[
  {"path":"dist/app.tar.gz","type":"Archive"},
  {"path":"dist/checksums.sha256","type":"Checksum"}
]`)

	explicit := apprelease.FileManifest{
		Version: appreleaseVersionForTest,
		Assets: []apprelease.FileEntry{
			{Path: "dist/app.tar.gz", Name: "app.tar.gz", Source: "explicit"},
			{Path: "dist/checksums.sha256", Name: "checksums.sha256", Source: "explicit"},
			{Path: "dist/doctor.json", Name: "doctor.json", Source: "explicit"},
		},
		Checksums:  []apprelease.FileEntry{{Path: "dist/checksums.sha256", Name: "checksums.sha256", Source: "checksum"}},
		SBOMs:      []apprelease.FileEntry{},
		Evidence:   []apprelease.FileEntry{{Path: "dist/doctor.json", Name: "doctor.json", Source: "evidence"}},
		Provenance: []apprelease.FileEntry{},
	}

	body, err := json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}

	writeReleaseFilesTestFile(t, "dist/release-files.json", string(body))

	manifest, err := apprelease.WriteReleaseFileManifest(apprelease.WriteReleaseFileManifestInput{
		FilesInput: apprelease.FilesInput{DistDir: "dist", ManifestFile: "dist/release-files.json"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(releaseFileEntryNames(manifest.Evidence), []string{"doctor.json"}) {
		t.Fatalf("evidence = %#v, want preserved doctor.json", manifest.Evidence)
	}

	assertReleaseAssetsInclude(t, manifest.Assets,
		"app.tar.gz", "checksums.sha256", "checksums.sha256.bundle",
		"doctor.json", "doctor.json.bundle",
		"slsa-provenance.intoto.json", "slsa-provenance.intoto.json.bundle",
	)

	if source := releaseFileEntrySource(manifest.Assets, "doctor.json"); source != "explicit" {
		t.Fatalf("doctor.json source = %q, want explicit", source)
	}

	if err := apprelease.ValidateReleaseFileManifest(apprelease.FilesInput{DistDir: "dist", ManifestFile: "dist/release-files.json"}); err != nil {
		t.Fatalf("validate finalized manifest: %v", err)
	}

	firstWrite := readFile(t, "dist/release-files.json")
	if _, err := apprelease.WriteReleaseFileManifest(apprelease.WriteReleaseFileManifestInput{
		FilesInput: apprelease.FilesInput{DistDir: "dist", ManifestFile: "dist/release-files.json"},
	}); err != nil {
		t.Fatalf("repeat manifest finalization: %v", err)
	}

	if secondWrite := readFile(t, "dist/release-files.json"); secondWrite != firstWrite {
		t.Fatal("repeated manifest finalization changed the finalized manifest")
	}
}

func TestWriteReleaseFileManifest_RejectsParentSymlink(t *testing.T) {
	t.Chdir(t.TempDir())

	outside := t.TempDir()
	writeReleaseFilesTestFile(t, "dist/checksums.txt", strings.Repeat("0", 64)+"  secret.tgz\n")
	writeReleaseFilesTestFile(t, filepath.Join(outside, "secret.tgz"), "outside\n")

	if err := os.Symlink(outside, "dist/link"); err != nil {
		t.Fatal(err)
	}

	manifest := apprelease.FileManifest{
		Version: appreleaseVersionForTest,
		Assets: []apprelease.FileEntry{
			{Path: "dist/checksums.txt", Name: "checksums.txt", Source: "explicit"},
			{Path: "dist/link/secret.tgz", Name: "secret.tgz", Source: "explicit"},
		},
		Checksums: []apprelease.FileEntry{{Path: "dist/checksums.txt", Name: "checksums.txt", Source: "checksum"}},
		Evidence:  []apprelease.FileEntry{{Path: "dist/link/secret.tgz", Name: "secret.tgz", Source: "evidence"}},
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	writeReleaseFilesTestFile(t, "dist/release-files.json", string(body))

	_, err = apprelease.WriteReleaseFileManifest(apprelease.WriteReleaseFileManifestInput{
		FilesInput: apprelease.FilesInput{DistDir: "dist", ManifestFile: "dist/release-files.json"},
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "dist/link") || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("err = %v, want parent symlink refusal", err)
	}
}

func TestReleaseFileManifest_RejectsDuplicateSectionPath(t *testing.T) {
	t.Chdir(t.TempDir())
	writeReleaseFilesTestFile(t, "dist/checksums.txt", "checksums\n")

	entry := apprelease.FileEntry{Path: "dist/checksums.txt", Name: "checksums.txt", Source: "checksum"}
	manifest := apprelease.FileManifest{
		Version:   appreleaseVersionForTest,
		Assets:    []apprelease.FileEntry{entry},
		Checksums: []apprelease.FileEntry{entry, entry},
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	writeReleaseFilesTestFile(t, "dist/release-files.json", string(body))

	err = apprelease.ValidateReleaseFileManifest(apprelease.FilesInput{DistDir: "dist", ManifestFile: "dist/release-files.json"})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "checksums contains duplicate path dist/checksums.txt") {
		t.Fatalf("err = %v, want duplicate checksum path refusal", err)
	}
}

func TestReleaseFileManifest_RejectsNonFilePathSpelling(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		distDir      string
		manifestPath string
		assetPath    string
		assetName    string
		setup        func(*testing.T)
	}{
		{
			name:    "current directory",
			distDir: ".", manifestPath: "release-files.json",
			assetPath: "./", assetName: ".",
			setup: func(t *testing.T) { t.Helper() },
		},
		{
			name:    "regular file with trailing separator",
			distDir: "dist", manifestPath: "dist/release-files.json",
			assetPath: "dist/checksums.txt/", assetName: "checksums.txt",
			setup: func(t *testing.T) {
				t.Helper()
				writeReleaseFilesTestFile(t, "dist/checksums.txt", "checksums\n")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			testCase.setup(t)

			entry := apprelease.FileEntry{Path: testCase.assetPath, Name: testCase.assetName, Source: "checksum"}
			manifest := apprelease.FileManifest{
				Version: appreleaseVersionForTest, Assets: []apprelease.FileEntry{entry}, Checksums: []apprelease.FileEntry{entry},
			}

			body, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}

			writeReleaseFilesTestFile(t, testCase.manifestPath, string(body))

			err = apprelease.ValidateReleaseFileManifest(apprelease.FilesInput{DistDir: testCase.distDir, ManifestFile: testCase.manifestPath})
			if !errors.Is(err, errs.ErrMissingInput) {
				t.Fatalf("err = %v, want non-file path refusal", err)
			}
		})
	}
}

func TestReleaseFileManifest_RejectsImageLedger(t *testing.T) {
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
		checksumLines = append(checksumLines, testFileSHA256(t, filepath.Join("dist", file))+"  "+file)
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

func testFileSHA256(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return fmt.Sprintf("%x", sha256.Sum256(body))
}

// assertReleaseAssetsInclude reports every name missing from entries, rather
// than stopping at the first, so one run names the whole gap.
func assertReleaseAssetsInclude(t *testing.T, entries []apprelease.FileEntry, names ...string) {
	t.Helper()

	for _, name := range names {
		if !releaseFileEntryNamed(entries, name) {
			t.Errorf("expected release asset %q in %#v", name, releaseFileEntryNames(entries))
		}
	}
}

// assertReleaseAssetsExclude is the counterpart: every name that leaked into
// the published set is reported.
func assertReleaseAssetsExclude(t *testing.T, entries []apprelease.FileEntry, names ...string) {
	t.Helper()

	for _, name := range names {
		if releaseFileEntryNamed(entries, name) {
			t.Errorf("unexpected release asset %q", name)
		}
	}
}

// requireChecksumFileRejected appends one more line naming subject to the
// fixture's checksum file and requires validation to refuse the result as a
// validation error mentioning wantReason.
func requireChecksumFileRejected(t *testing.T, name, subject, wantReason string) {
	t.Helper()

	writeReleaseFilesTestFile(t, name, readFile(t, "dist/app_checksums.txt")+strings.Repeat("a", 64)+"  "+subject+"\n")

	err := apprelease.ValidateReleaseChecksums(apprelease.ValidateReleaseChecksumsInput{ChecksumsFile: name})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), wantReason) {
		t.Fatalf("%s: err = %v, want validation error mentioning %q", name, err, wantReason)
	}
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

func releaseFileEntrySource(entries []apprelease.FileEntry, name string) string {
	for _, entry := range entries {
		if entry.Name == name {
			return entry.Source
		}
	}

	return ""
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
