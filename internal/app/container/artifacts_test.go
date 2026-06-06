// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestValidateArtifacts_MavenFindsJARs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("demo-1.0.0.jar", []byte("fake-jar"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY demo-1.0.0.jar /app/\n"))

	var out bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "maven", ArtifactDir: fsys.Root, ContainerfilePath: cf, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "✓ Maven artifacts found:") {
		t.Errorf("missing success header:\n%s", out.String())
	}
}

func TestValidateArtifacts_GoFindsNestedBinaries(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "linux-amd64", "demo-linux-amd64"), []byte("binary"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY dist/linux-amd64/demo-linux-amd64 /app/\n"))

	var out bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "go", ArtifactDir: fsys.Path("dist"), ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "✓ Go artifacts found:") || !strings.Contains(out.String(), "demo-linux-amd64") {
		t.Errorf("missing Go artifact output:\n%s", out.String())
	}
}

// TestValidateArtifacts_CargoFindsNestedBinaries is the Cargo counterpart
// — artefact-first cargo lands binaries in the same dist/<goos>-<goarch>/...
// layout as Go, so the recursive scan must find them and report under
// the "Cargo" type label.
func TestValidateArtifacts_CargoFindsNestedBinaries(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile(filepath.Join("dist", "linux-amd64", "demo-linux-amd64"), []byte("binary"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY dist/linux-amd64/demo-linux-amd64 /app/\n"))

	var out bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "cargo", ArtifactDir: fsys.Path("dist"), ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "✓ Cargo artifacts found:") || !strings.Contains(out.String(), "demo-linux-amd64") {
		t.Errorf("missing Cargo artifact output:\n%s", out.String())
	}
}

func TestValidateArtifacts_UnknownTypeErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	if err := appcontainer.ValidateArtifacts(io.Discard, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "rust", ArtifactDir: fsys.Root,
	}); err == nil {
		t.Fatal("expected error")
	}
}

// TestValidateArtifacts_MissingArtifactsFail is the deployable-pipeline
// guarantee: every Verify step in publish-container.yml only runs when
// the per-ecosystem artefact-name is non-empty (i.e. an artefact-first
// dep is declared). When the step runs, the artefact MUST be present —
// a missing one is an upstream build failure, not "rebuild from source."
// The old soft-warn path masked these as yellow warnings; the verifier
// now errors with ErrValidation.
func TestValidateArtifacts_MissingArtifactsFail(t *testing.T) {
	tests := []struct {
		name        string
		projectType string
		wantStderr  []string
	}{
		{
			name:        "npm",
			projectType: "npm",
			wantStderr:  []string{"::error::No NPM artifacts found"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		{
			name:        "maven",
			projectType: "maven",
			wantStderr:  []string{"::error::No Maven artifacts found"},
		},
		{
			name:        "go",
			projectType: "go",
			wantStderr:  []string{"::error::No Go artifacts found"},
		},
		{
			name:        "cargo",
			projectType: "cargo",
			wantStderr:  []string{"::error::No Cargo artifacts found"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)

			var out, stderr bytes.Buffer

			err := appcontainer.ValidateArtifacts(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
				ProjectType: testCase.projectType,
				ArtifactDir: fsys.Root,
			})
			if err == nil {
				t.Fatalf("expected error when %s artefacts are missing, got nil", testCase.projectType)
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want wrapped ErrValidation", err)
			}

			for _, want := range testCase.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("missing %q in stderr:\n%s", want, stderr.String())
				}
			}
		})
	}
}

func TestValidateArtifacts_RebuildWarningIncludesGuidanceAndPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("demo.jar", []byte("fake"))
	cf := fsys.WriteFile("Dockerfile", []byte("FROM maven:3\nRUN apt-get update && \\\n    mvn clean package && \\\n    rm -rf /root/.m2"))

	var out, stderr bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
		ProjectType:       "maven",
		ArtifactDir:       fsys.Root,
		ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"::warning::Containerfile rebuilds from source", "downloaded artifacts may be ignored", "COPY pre-built artifacts", "Dockerfile"} {
		if !strings.Contains(out.String(), want) && !strings.Contains(stderr.String(), want) {
			t.Errorf("missing %q\nstdout:\n%s\nstderr:\n%s", want, out.String(), stderr.String())
		}
	}
}

func TestValidateArtifacts_DirWithSpaces(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("my artifacts")
	fsys.WriteFile(filepath.Join("my artifacts", "app.jar"), []byte("jar"))

	if err := appcontainer.ValidateArtifacts(io.Discard, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "maven",
		ArtifactDir: dir,
	}); err != nil {
		t.Fatal(err)
	}
}
