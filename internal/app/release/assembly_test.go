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
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestAssemble_StagesCanonicalReleaseFiles(t *testing.T) {
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
		t.Fatalf("assets = %v, want %v", gotAssets, wantAssets)
	}

	gotSBOMs := assemblyFileNames(asm.SBOMs)

	wantSBOMs := []string{"api-sbom.spdx.json", "web-analyzed-container-sbom.spdx.json"}
	if !reflect.DeepEqual(gotSBOMs, wantSBOMs) {
		t.Fatalf("sboms = %v, want %v", gotSBOMs, wantSBOMs)
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

	if _, err := os.Stat(domainrelease.DefaultAssemblyFile); err != nil {
		t.Errorf("assembly manifest missing: %v", err)
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

func TestAssemblyConsumersUseManifestFiles(t *testing.T) {
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
		ChecksumFile: "release-files/checksums.sha256",
		SBOMZipFile:  "release-files/assets/my-app-1.2.3-sboms.zip",
	})

	zipRes, err := apprelease.CreateSBOMZip(context.Background(), nil, apprelease.SBOMZipInput{
		ProjectName:  "my-app",
		Version:      "v1.2.3",
		AssemblyFile: domainrelease.DefaultAssemblyFile,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if zipRes.EntryCount != 1 || zipRes.ZipName != "release-files/assets/my-app-1.2.3-sboms.zip" {
		t.Fatalf("SBOM zip result = %+v", zipRes)
	}

	assertZipEntries(t, zipRes.ZipName, []string{"api-sbom.spdx.json"})

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{AssemblyFile: domainrelease.DefaultAssemblyFile})
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("checksum count = %d, want 2", count)
	}

	signer := &fakeSigner{}
	if err := apprelease.SignArtifacts(context.Background(), signer, &bytes.Buffer{}, apprelease.SignInput{AssemblyFile: domainrelease.DefaultAssemblyFile}); err != nil {
		t.Fatal(err)
	}

	wantSigned := []string{
		"release-files/assets/app.jar",
		"release-files/assets/my-app-1.2.3-sboms.zip",
		"release-files/checksums.sha256",
	}
	if !reflect.DeepEqual(signer.signed, wantSigned) {
		t.Fatalf("signed = %v, want %v", signer.signed, wantSigned)
	}

	prov := fakeprovider.New(t)
	if err := apprelease.CreateRelease(context.Background(), prov, &fakeFS{Files: map[string]bool{}}, &bytes.Buffer{}, apprelease.CreateReleaseInput{
		Tag:          "v1.2.3",
		Repository:   "owner/repo",
		AssemblyFile: domainrelease.DefaultAssemblyFile,
	}); err != nil {
		t.Fatal(err)
	}

	assets := prov.CreateReleaseCalls()[0].Spec.Assets

	wantAssets := []string{
		"release-files/assets/app.jar",
		"release-files/assets/app.jar.asc",
		"release-files/assets/my-app-1.2.3-sboms.zip",
		"release-files/assets/my-app-1.2.3-sboms.zip.asc",
		"release-files/checksums.sha256",
		"release-files/checksums.sha256.asc",
	}
	if !reflect.DeepEqual(assets, wantAssets) {
		t.Fatalf("release assets = %v, want %v", assets, wantAssets)
	}
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

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zip entries = %v, want %v", got, want)
	}
}
