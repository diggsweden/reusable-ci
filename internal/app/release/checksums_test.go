// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestChecksums_ReleaseArtifacts(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(filepath.Join("release-artifacts", "app.jar"), []byte("hi"))
	fsys.WriteFile(filepath.Join("release-artifacts", "app.tgz"), []byte("yo"))

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{})
	if err != nil {
		t.Fatalf("Checksums: %v", err)
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// Manifest entries use basename labels.
	data, err := os.ReadFile("checksums.sha256")
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}

	for _, line := range lines {
		// Lines look like "<64hex>  <name>"
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			t.Errorf("malformed line: %q", line)

			continue
		}

		hash, name := parts[0], parts[1]
		if len(hash) != 64 {
			t.Errorf("hash len = %d, want 64: %q", len(hash), hash)
		}

		if !strings.HasSuffix(name, ".jar") && !strings.HasSuffix(name, ".tgz") {
			t.Errorf("unexpected basename label: %q", name)
		}
	}
}

func TestChecksums_AttachArtifactsKeepsPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(filepath.Join("extra", "binary-amd64"), []byte("x"))

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{
		AttachArtifacts: "extra/*",
	})
	if err != nil {
		t.Fatalf("Checksums: %v", err)
	}

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	data, _ := os.ReadFile("checksums.sha256")
	if !strings.Contains(string(data), "extra/binary-amd64") {
		t.Errorf("manifest should keep original path, got: %q", data)
	}
}

func TestChecksums_SBOMs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("my-app-sbom.spdx.json", []byte(`{"spdx":1}`))
	fsys.WriteFile("my-app-sbom.cyclonedx.json", []byte(`{"cdx":1}`))

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{})
	if err != nil {
		t.Fatalf("Checksums: %v", err)
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestChecksums_AnalyzedContainerSBOMs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile(filepath.Join("sbom-artifacts", "my-app-analyzed-container-sbom.spdx.json"), []byte(`{}`))

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{})
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestChecksums_CreatesEmptyOutputWhenNothingFound(t *testing.T) {
	tests := []struct {
		name  string
		input apprelease.ChecksumsInput
	}{
		{name: "default_dirs_empty"},
		{name: "missing_dirs", input: apprelease.ChecksumsInput{ReleaseArtifactsDir: "./nonexistent", SBOMDir: "./missing-sbom"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			var out bytes.Buffer

			count, err := apprelease.Checksums(&out, testCase.input)
			if err != nil {
				t.Fatalf("Checksums: %v", err)
			}

			if count != 0 {
				t.Errorf("count = %d, want 0", count)
			}

			if _, err := os.Stat("checksums.sha256"); err != nil {
				t.Errorf("output file should still be created (empty): %v", err)
			}

			if !strings.Contains(out.String(), "Generated 0 checksums") {
				t.Errorf("out = %q", out.String())
			}
		})
	}
}

func TestChecksums_CustomOutput(t *testing.T) {
	tests := []struct {
		name       string
		outputFile string
		wantPath   []string
	}{
		{name: "file", outputFile: "custom-checksums.txt", wantPath: []string{"custom-checksums.txt"}},
		{name: "nested_path", outputFile: filepath.Join("output", "checksums.sha256"), wantPath: []string{"output", "checksums.sha256"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			if len(testCase.wantPath) > 1 {
				fsys.MkdirAll(testCase.wantPath[:len(testCase.wantPath)-1]...)
			}

			fsys.WriteFile(filepath.Join("release-artifacts", "test.jar"), []byte("test"))

			count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{OutputFile: testCase.outputFile})
			if err != nil {
				t.Fatal(err)
			}

			if count != 1 {
				t.Errorf("count = %d, want 1", count)
			}

			if _, err := os.Stat(fsys.Path(testCase.wantPath...)); err != nil {
				t.Errorf("custom output missing: %v", err)
			}

			if _, err := os.Stat("checksums.sha256"); !os.IsNotExist(err) {
				t.Errorf("default output should not exist when custom path used: %v", err)
			}
		})
	}
}

func TestChecksums_CommaSeparatedAttachPatterns(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("file1.txt", []byte("file1"))
	fsys.WriteFile("file2.md", []byte("file2"))

	count, err := apprelease.Checksums(&bytes.Buffer{}, apprelease.ChecksumsInput{AttachArtifacts: "file1.txt,file2.md"})
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	body, _ := os.ReadFile("checksums.sha256")
	for _, want := range []string{"file1.txt", "file2.md"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %q in %q", want, body)
		}
	}
}
