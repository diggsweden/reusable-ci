// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestAssemble_StagesEveryProducerLayoutIntoOneManifest feeds Assemble the
// shapes different producers leave behind — a Maven target directory, loose Go
// binaries, an extracted binaries tree, SBOMs beside their service and in a
// scan output directory, an attached file matched by glob — and requires all of
// it to land in one staged release-files layout described by one manifest.
//
// The manifest file is asserted, not just its existence. Assemble exits after
// writing it and a later command reads it back, so the file is the handoff;
// the struct Assemble returned is not what anything downstream sees.
func TestAssemble_StagesEveryProducerLayoutIntoOneManifest(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile(filepath.Join("release-artifacts", "target", "app.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("release-artifacts", "target", "original-app.jar"), []byte("original"))
	fsys.WriteFile(filepath.Join("release-artifacts", "service-linux-amd64"), []byte("go binary"))
	fsys.WriteFile(filepath.Join("release-artifacts", "app.apk"), []byte("apk"))
	fsys.WriteFile(filepath.Join("release-artifacts", "app.ipa"), []byte("ipa"))
	fsys.WriteFile(filepath.Join("release-artifacts", "binaries", "darwin", "service-darwin-arm64"), []byte("extracted"))
	fsys.WriteFile(filepath.Join("release-artifacts", "release-images.json"), []byte(`{}`))
	fsys.WriteFile(filepath.Join("release-artifacts", "release-images-ledger.json"), []byte(`{}`))
	fsys.WriteFile(filepath.Join("extra", "nested", "readme.txt"), []byte("attached"))
	fsys.WriteFile(filepath.Join("services", "api", "api-sbom.spdx.json"), []byte(`{"spdxVersion":"2.3"}`))
	fsys.WriteFile(filepath.Join("sbom-artifacts", "web-analyzed-container-sbom.spdx.json"), []byte(`{}`))

	asm, err := apprelease.Assemble(&bytes.Buffer{}, apprelease.AssembleInput{
		ConfigPlanJSON:           releaseAssemblyConfigJSON(t),
		ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t),
		AttachArtifacts:          "extra/**/*.txt",
		ProjectName:              "my-app",
		Version:                  "v1.2.3",
	})
	if err != nil {
		t.Fatal(err)
	}

	gotAssets := assemblyFileNames(asm.Assets)

	wantAssets := []string{"app.apk", "app.ipa", "app.jar", "readme.txt", "service-darwin-arm64", "service-linux-amd64"}
	if !reflect.DeepEqual(gotAssets, wantAssets) {
		t.Errorf("assets = %v, want %v", gotAssets, wantAssets)
	}

	gotSBOMs := assemblyFileNames(asm.SBOMs)

	wantSBOMs := []string{"api-sbom.spdx.json", "web-analyzed-container-sbom.spdx.json"}
	if !reflect.DeepEqual(gotSBOMs, wantSBOMs) {
		t.Errorf("sboms = %v, want %v", gotSBOMs, wantSBOMs)
	}

	if asm.ChecksumFile != "release-files/checksums.sha256" {
		t.Errorf("checksum file = %q", asm.ChecksumFile)
	}

	if asm.SBOMZipFile != "release-files/assets/my-app-1.2.3-sboms.zip" {
		t.Errorf("SBOM zip file = %q", asm.SBOMZipFile)
	}

	for _, name := range wantAssets {
		if _, err := os.Stat(filepath.Join("release-files", "assets", name)); err != nil {
			t.Errorf("staged asset %s missing: %v", name, err)
		}
	}

	// The written manifest is the handoff to every later command, and it
	// carries the source_* provenance fields that exist only for auditing.
	// Round-tripping it covers all of that without restating field by field.
	var onDisk domainrelease.Assembly
	if err := json.Unmarshal([]byte(readFile(t, domainrelease.DefaultAssemblyFile)), &onDisk); err != nil {
		t.Fatalf("assembly manifest is missing or not valid JSON: %v", err)
	}

	if !reflect.DeepEqual(&onDisk, asm) {
		t.Errorf("manifest on disk = %+v\nAssemble returned  %+v", onDisk, *asm)
	}
}

func TestAssemble_RejectsDuplicateAssetBasenames(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile(filepath.Join("release-artifacts", "one", "app.jar"), []byte("one"))
	fsys.WriteFile(filepath.Join("release-artifacts", "two", "app.jar"), []byte("two"))

	_, err := apprelease.Assemble(&bytes.Buffer{}, apprelease.AssembleInput{
		ConfigPlanJSON:           releaseAssemblyConfigJSON(t),
		ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t),
		ProjectName:              "my-app",
		Version:                  "v1.2.3",
	})
	if err == nil || !strings.Contains(err.Error(), "basename") {
		t.Fatalf("err = %v, want duplicate basename error", err)
	}
}

func TestAssemble_RejectsUnsafeAttachmentPattern(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	_, err := apprelease.Assemble(&bytes.Buffer{}, apprelease.AssembleInput{
		ConfigPlanJSON:           releaseAssemblyConfigJSON(t),
		ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t),
		AttachArtifacts:          "../dist/*.zip",
		ProjectName:              "my-app",
		Version:                  "v1.2.3",
	})
	if err == nil || !strings.Contains(err.Error(), "must be relative") && !strings.Contains(err.Error(), "contains") {
		t.Fatalf("err = %v, want unsafe attachment pattern error", err)
	}
}

// TestAssembly_EveryStageTakesItsFilesFromTheManifest runs the four consumers
// of an assembly manifest in the order a release runs them: zip the SBOMs,
// checksum what ships, sign it, publish it. The claim they share is that none
// of them discovers files for itself — each file set comes from the manifest.
//
// Every stage writes what the next one reads, so they share a fixture and run
// in order. They are subtests because they are four separate claims: as one
// linear run of t.Fatal calls the first failure hid the rest, and the name had
// become a list of the stages rather than the behaviour they share.
func TestAssembly_EveryStageTakesItsFilesFromTheManifest(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile(filepath.Join("release-files", "assets", "app.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("release-files", "sboms", "api-sbom.spdx.json"), []byte(`{"spdxVersion":"2.3"}`))

	writeAssembly(t, domainrelease.DefaultAssemblyFile, domainrelease.Assembly{
		Version: domainrelease.AssemblyVersion,
		Assets: []domainrelease.AssemblyFile{
			{Path: "release-files/assets/app.jar", Name: "app.jar", Required: true},
		},
		SBOMs: []domainrelease.AssemblyFile{
			{Path: "release-files/sboms/api-sbom.spdx.json", Name: "api-sbom.spdx.json", Required: true},
		},
		ChecksumFile: assemblyChecksumPath,
		SBOMZipFile:  assemblySBOMZipPath,
	})

	t.Run("zips the SBOMs the manifest lists, under the name it chose", func(t *testing.T) {
		zipRes, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
			ProjectName:  "my-app",
			Version:      "v1.2.3",
			AssemblyFile: domainrelease.DefaultAssemblyFile,
		}, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}

		if zipRes.ZipName != assemblySBOMZipPath || zipRes.EntryCount != 1 {
			t.Errorf("SBOM zip result = %+v", zipRes)
		}

		assertZipEntries(t, assemblySBOMZipPath, []string{"api-sbom.spdx.json"})
	})

	t.Run("checksums what ships, naming each subject as it is published", func(t *testing.T) {
		count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{AssemblyFile: domainrelease.DefaultAssemblyFile})
		if err != nil {
			t.Fatal(err)
		}

		if count != 2 {
			t.Errorf("checksum count = %d, want 2", count)
		}

		// The written file is what a consumer runs sha256sum --check
		// against, so the subjects it names are the contract. A count
		// alone cannot tell the SBOM zip from the SBOM it was made from.
		want := []string{"app.jar", "my-app-1.2.3-sboms.zip"}
		if got := checksumSubjects(t, assemblyChecksumPath); !reflect.DeepEqual(got, want) {
			t.Errorf("checksum subjects = %v, want %v", got, want)
		}
	})

	t.Run("signs the assets, the SBOM zip and the checksum file", func(t *testing.T) {
		signer := &fakeSigner{}
		if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{AssemblyFile: domainrelease.DefaultAssemblyFile}); err != nil {
			t.Fatal(err)
		}

		wantSigned := []string{
			"release-files/assets/app.jar",
			assemblySBOMZipPath,
			assemblyChecksumPath,
		}
		if !reflect.DeepEqual(signer.signed, wantSigned) {
			t.Errorf("signed = %v, want %v", signer.signed, wantSigned)
		}
	})

	t.Run("publishes every signed file alongside its signature", func(t *testing.T) {
		prov := fakeprovider.New(t)
		if err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
			Tag:          "v1.2.3",
			Repository:   "owner/repo",
			AssemblyFile: domainrelease.DefaultAssemblyFile,
		}); err != nil {
			t.Fatal(err)
		}

		wantAssets := []string{
			"release-files/assets/app.jar",
			"release-files/assets/app.jar.asc",
			assemblySBOMZipPath,
			assemblySBOMZipPath + ".asc",
			assemblyChecksumPath,
			assemblyChecksumPath + ".asc",
		}
		if assets := prov.CreateReleaseCalls()[0].Spec.Assets; !reflect.DeepEqual(assets, wantAssets) {
			t.Errorf("release assets = %v, want %v", assets, wantAssets)
		}
	})
}

func TestChecksums_AssemblyRequiresSBOMZipWhenSBOMInputsExist(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	fsys.WriteFile(filepath.Join("release-files", "assets", "app.jar"), []byte("jar"))
	fsys.WriteFile(filepath.Join("release-files", "sboms", "api-sbom.spdx.json"), []byte(`{}`))

	writeAssembly(t, domainrelease.DefaultAssemblyFile, domainrelease.Assembly{
		Version: domainrelease.AssemblyVersion,
		Assets: []domainrelease.AssemblyFile{
			{Path: "release-files/assets/app.jar", Name: "app.jar", Required: true},
		},
		SBOMs: []domainrelease.AssemblyFile{
			{Path: "release-files/sboms/api-sbom.spdx.json", Name: "api-sbom.spdx.json", Required: true},
		},
		ChecksumFile: "release-files/checksums.sha256",
		SBOMZipFile:  "release-files/assets/my-app-1.2.3-sboms.zip",
	})

	_, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{AssemblyFile: domainrelease.DefaultAssemblyFile})
	if err == nil || !strings.Contains(err.Error(), "run release sbom-zip --assembly first") {
		t.Fatalf("err = %v, want missing SBOM ZIP error", err)
	}
}

func releaseAssemblyConfigJSON(t *testing.T) string {
	t.Helper()

	return mustReleaseJSON(t, pipeline.ConfigPlan{
		Version: pipeline.ConfigPlanVersion,
		Artifacts: pipeline.ArtifactSets{
			All: []pipeline.PlannedArtifact{
				{WorkingDirectory: "services/api"},
			},
			GoArtifactFirst: []pipeline.PlannedArtifact{
				{BuildArtifactName: "go-build-artifacts"},
			},
		},
	})
}

func releaseAssemblyTransferJSON(t *testing.T) string {
	t.Helper()

	return mustReleaseJSON(t, pipeline.ArtifactTransferPlan{
		Version: pipeline.ArtifactTransferPlanVersion,
		Items: []pipeline.ArtifactTransfer{
			{Kind: pipeline.ArtifactTransferBuildArtifact, Name: "build-artifacts", Path: "./release-artifacts", Required: true},
			{Kind: pipeline.ArtifactTransferExtractedBinaries, Name: "extracted-binaries", Path: "./release-artifacts/binaries", Required: false},
			{Kind: pipeline.ArtifactTransferAnalyzedContainerSBOM, NameTemplate: "analyzed-container-sbom-{run_id}", Path: "./sbom-artifacts", Required: false},
		},
	})
}

// The two files an assembly manifest names for the release to generate. Every
// stage after Assemble takes them from the manifest rather than deriving them,
// so the test states each path once and asserts against that.
const (
	assemblyChecksumPath = "release-files/checksums.sha256"
	assemblySBOMZipPath  = "release-files/assets/my-app-1.2.3-sboms.zip"
)

// checksumSubjects returns the file names a sha256sum manifest lists, in the
// order it lists them. sha256sum separates digest from subject with two spaces.
func checksumSubjects(t *testing.T, path string) []string {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(readFile(t, path)), "\n")

	subjects := make([]string, 0, len(lines))

	for _, line := range lines {
		_, subject, found := strings.Cut(line, "  ")
		if !found {
			t.Fatalf("%s: malformed checksum line %q", path, line)
		}

		subjects = append(subjects, subject)
	}

	return subjects
}

func assemblyFileNames(files []domainrelease.AssemblyFile) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, file.Name)
	}

	return out
}

func writeAssembly(t *testing.T, path string, asm domainrelease.Assembly) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir assembly dir: %v", err)
	}

	if err := os.WriteFile(path, []byte(mustReleaseJSON(t, asm)), 0o600); err != nil {
		t.Fatalf("write assembly: %v", err)
	}
}

func mustReleaseJSON(t *testing.T, value any) string {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}

	return string(body)
}

// assertZipEntries requires the archive to hold exactly these entries. Both
// sides are sorted first: nothing reads these archives in order, so entry order
// is discovery order rather than a contract, and pinning it would fail on a
// harmless change to how SBOMs are found.
func assertZipEntries(t *testing.T, zipName string, want []string) {
	t.Helper()

	reader, err := zip.OpenReader(zipName)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reader.Close() }()

	got := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		got = append(got, file.Name)
	}

	sort.Strings(got)

	wantSorted := slices.Clone(want)
	sort.Strings(wantSorted)

	if !reflect.DeepEqual(got, wantSorted) {
		t.Errorf("zip entries = %v, want %v", got, wantSorted)
	}
}
