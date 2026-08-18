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

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// TestValidateArtifacts_JVMFindsJARs covers the two project types that look
// for JARs at the top of the uploaded directory. Gradle had no test at all,
// in either direction, and shares every rule here with Maven.
func TestValidateArtifacts_JVMFindsJARs(t *testing.T) {
	for projectType, label := range map[string]string{
		"maven":  "Maven",
		"gradle": "Gradle",
	} {
		t.Run(projectType, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("demo-1.0.0.jar", []byte("fake-jar"))
			cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY demo-1.0.0.jar /app/\n"))

			var out bytes.Buffer
			if err := appcontainer.ValidateArtifacts(&out, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
				ProjectType: projectType, ArtifactDir: fsys.Root, ContainerfilePath: cf,
			}); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(out.String(), "✓ "+label+" artifacts found:") {
				t.Errorf("missing success header:\n%s", out.String())
			}
		})
	}
}

// TestValidateArtifacts_TopLevelTypesDoNotRecurse pins the boundary between the
// two scanning rules. Go and Cargo recurse, because their binaries land under
// dist/<os>-<arch>/. The JVM and NPM types deliberately do not, and nothing
// tested that: an npm directory recursed would match the first file inside
// node_modules and report artifacts for a build that produced none.
func TestValidateArtifacts_TopLevelTypesDoNotRecurse(t *testing.T) {
	for _, projectType := range []string{"maven", "gradle", "npm"} {
		t.Run(projectType, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile(filepath.Join("node_modules", "left-pad", "index.jar"), []byte("not an artifact"))

			var out, stderr bytes.Buffer

			err := appcontainer.ValidateArtifacts(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
				ProjectType: projectType, ArtifactDir: fsys.Root,
			})
			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want a validation error: a nested file is not a %s artifact", err, projectType)
			}
		})
	}
}

// TestValidateArtifacts_NPMAcceptsAnyTopLevelFile is the npm counterpart to the
// JVM test. npm has no extension to match on -- the shell this replaced globbed
// "*" -- so any file directly in the uploaded directory counts, and npm was
// covered only by its failure case.
func TestValidateArtifacts_NPMAcceptsAnyTopLevelFile(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("bundle.tgz", []byte("packed"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY bundle.tgz /app/\n"))

	var out bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "npm", ArtifactDir: fsys.Root, ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "✓ NPM artifacts found:") || !strings.Contains(out.String(), "bundle.tgz") {
		t.Errorf("missing NPM artifact output:\n%s", out.String())
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
// — artifact-first cargo lands binaries in the same dist/<goos>-<goarch>/...
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
// the per-ecosystem artifact-name is non-empty (i.e. an artifact-first
// dep is declared). When the step runs, the artifact MUST be present —
// a missing one is an upstream build failure, not "rebuild from source."
// The old soft-warn path masked these as yellow warnings; the verifier
// now errors with ErrValidation.
// TestValidateArtifacts_MissingArtifactsFail covers every project type with an
// empty directory. Each names its own ecosystem in the error, so a row that
// passed because a different validator ran would show up.
func TestValidateArtifacts_MissingArtifactsFail(t *testing.T) {
	for projectType, wantStderr := range map[string]string{
		"npm":    "::error::No NPM artifacts found",
		"maven":  "::error::No Maven artifacts found",
		"gradle": "::error::No Gradle artifacts found",
		"go":     "::error::No Go artifacts found",
		"cargo":  "::error::No Cargo artifacts found",
	} {
		t.Run(projectType, func(t *testing.T) {
			fsys := testfs.NewReal(t)

			var out, stderr bytes.Buffer

			err := appcontainer.ValidateArtifacts(&out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
				ProjectType: projectType,
				ArtifactDir: fsys.Root,
			})
			if err == nil {
				t.Fatalf("expected an error when %s artifacts are missing, got nil", projectType)
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want wrapped ErrValidation", err)
			}

			if !strings.Contains(stderr.String(), wantStderr) {
				t.Errorf("missing %q in stderr:\n%s", wantStderr, stderr.String())
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
