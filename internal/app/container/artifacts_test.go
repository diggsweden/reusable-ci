// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
			if err := appcontainer.ValidateArtifacts(&out, output.Annotator{}, appcontainer.ValidateArtifactsInput{
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

			err := appcontainer.ValidateArtifacts(&out, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
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
	if err := appcontainer.ValidateArtifacts(&out, output.Annotator{}, appcontainer.ValidateArtifactsInput{
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
	if err := appcontainer.ValidateArtifacts(&out, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "go", ArtifactDir: fsys.Path("dist"), ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "✓ Go artifacts found:") || !strings.Contains(out.String(), "demo-linux-amd64") {
		t.Errorf("missing Go artifact output:\n%s", out.String())
	}
}

// TestValidateArtifacts_ReportsEveryArtifactInPathOrder pins the whole
// success report rather than a header and one name. The binaries are written
// in the opposite order to their paths, and each has its own size, so a report
// in creation order, one that drops an entry, or one that prints another
// entry's size differs from the expected text. A clean run writes nothing but
// that report: no annotation, no rebuild warning.
func TestValidateArtifacts_ReportsEveryArtifactInPathOrder(t *testing.T) {
	fsys := testfs.NewReal(t)
	arm := fsys.WriteFile(filepath.Join("dist", "linux-arm64", "demo-linux-arm64"), []byte("arm"))
	amd := fsys.WriteFile(filepath.Join("dist", "linux-amd64", "demo-linux-amd64"), []byte("amd64-binary"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM scratch\nCOPY dist/ /app/\n"))

	var out, annotations bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&out, output.NewAnnotator(&annotations, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
		ProjectType: "go", ArtifactDir: fsys.Path("dist"), ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	want := "✓ Go artifacts found:\n" +
		fmt.Sprintf("  %10d  %s\n", 12, amd) +
		fmt.Sprintf("  %10d  %s\n", 3, arm)
	if out.String() != want {
		t.Errorf("report =\n%s\nwant\n%s", out.String(), want)
	}

	if annotations.Len() != 0 {
		t.Errorf("annotations = %q, want none", annotations.String())
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
	if err := appcontainer.ValidateArtifacts(&out, output.Annotator{}, appcontainer.ValidateArtifactsInput{
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

	err := appcontainer.ValidateArtifacts(io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "rust", ArtifactDir: fsys.Root,
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage — an unknown project type is a caller mistake, not a missing artifact", err)
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

			err := appcontainer.ValidateArtifacts(&out, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
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
	if err := appcontainer.ValidateArtifacts(&out, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
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

// TestValidateArtifacts_CopyOnlyContainerfileIsNotWarnedAbout is the negative
// control for the rebuild warning. Without it, a checker that warned on every
// Containerfile it could read would pass the positive test above, and the
// warning would be noise on exactly the layout the pipeline is trying to
// encourage.
//
// The detector is a substring match over the whole file rather than an
// instruction-aware parse, so a COPY line that merely names a build tool
// ("COPY gradle-build-out/app.jar") does trip it. This fixture stays clear of
// those words on purpose; it pins the intended case, not that limitation.
func TestValidateArtifacts_CopyOnlyContainerfileIsNotWarnedAbout(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("demo.jar", []byte("fake"))
	cf := fsys.WriteFile("Containerfile", []byte(
		"FROM eclipse-temurin:21-jre\n"+
			"WORKDIR /app\n"+
			"COPY demo.jar /app/demo.jar\n"+
			"USER 65532:65532\n"+
			"ENTRYPOINT [\"java\", \"-jar\", \"/app/demo.jar\"]\n"))

	var out, stderr bytes.Buffer

	if err := appcontainer.ValidateArtifacts(&out, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
		ProjectType:       "maven",
		ArtifactDir:       fsys.Root,
		ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}

	for _, unwanted := range []string{"rebuilds from source", "may be ignored", "COPY pre-built artifacts"} {
		if strings.Contains(out.String(), unwanted) || strings.Contains(stderr.String(), unwanted) {
			t.Errorf("a COPY-only Containerfile was warned about (%q)\nstdout:\n%s\nstderr:\n%s", unwanted, out.String(), stderr.String())
		}
	}
}

func TestValidateArtifacts_DirWithSpaces(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("my artifacts")
	fsys.WriteFile(filepath.Join("my artifacts", "app.jar"), []byte("jar"))

	var out bytes.Buffer

	if err := appcontainer.ValidateArtifacts(&out, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "maven",
		ArtifactDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	// The jar is actually found, not merely "no error": a scan that saw
	// nothing in a space-containing path would fail loudly instead, but a
	// scan that reported the wrong file would not.
	if !strings.Contains(out.String(), "app.jar") {
		t.Errorf("artifact under a path with spaces was not listed:\n%s", out.String())
	}
}

// TestValidateArtifacts_ZeroSizePolicy covers the empty-artifact refusal across
// the matcher types, and the mixed case that must NOT refuse.
//
// This verb exists so a container is never built around a missing artifact, and
// a truncated upload satisfies "a file is present" while shipping exactly the
// empty image the check is meant to prevent. The refusal is written against the
// LARGEST match, so the two halves are different rules: every match empty is a
// failure, and one non-empty match among empties is a pass. Neither had a test,
// so a check written per-file instead of over the maximum — refusing whenever
// any artifact is empty — would have passed too, and that one breaks real
// builds that legitimately produce an empty marker file alongside a real one.
func TestValidateArtifacts_ZeroSizePolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		projectType string
		files       map[string]string
		wantRefusal bool
	}{
		{
			name: "every jar is empty", projectType: "maven",
			files:       map[string]string{"a.jar": "", "b.jar": ""},
			wantRefusal: true,
		},
		{
			name: "one jar has content", projectType: "maven",
			files: map[string]string{"a.jar": "", "b.jar": "real"},
		},
		{
			name: "every npm file is empty", projectType: "npm",
			files:       map[string]string{"pkg.tgz": ""},
			wantRefusal: true,
		},
		{
			name: "every go binary is empty", projectType: "go",
			files:       map[string]string{"dist/linux-amd64/app": ""},
			wantRefusal: true,
		},
		{
			name: "one go binary has content", projectType: "go",
			files: map[string]string{"dist/linux-amd64/app": "", "dist/linux-arm64/app": "elf"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)

			// The artifact directory is a subdirectory so the Containerfile
			// is not itself a match: the npm matcher accepts any top-level
			// file, and Go recurses.
			artifactDir := fsys.MkdirAll("dist-root")
			for path, body := range tc.files {
				fsys.WriteFile(filepath.Join("dist-root", path), []byte(body))
			}

			cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\n"))

			var out, annotations bytes.Buffer

			err := appcontainer.ValidateArtifacts(&out,
				output.NewAnnotator(&annotations, output.FormatGitHub),
				appcontainer.ValidateArtifactsInput{
					ProjectType: tc.projectType, ArtifactDir: artifactDir, ContainerfilePath: cf,
				})

			if !tc.wantRefusal {
				if err != nil {
					t.Fatalf("a set containing a non-empty artifact was refused: %v", err)
				}

				return
			}

			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation for an all-empty artifact set", err)
			}

			// The diagnostic must say the artifacts are EMPTY, not that they
			// are missing: those send the operator to different places.
			if !strings.Contains(err.Error(), "empty") {
				t.Errorf("err = %v, want it to say the artifacts are empty", err)
			}

			if !strings.Contains(annotations.String(), "empty") {
				t.Errorf("annotations = %q, want the empty-artifact annotation", annotations.String())
			}
		})
	}
}

// TestValidateArtifacts_RefusesNonRegularMatches pins the failure class for a
// match that is not a regular file, which is distinct from "no match" and from
// "empty".
func TestValidateArtifacts_RefusesNonRegularMatches(t *testing.T) {
	for name, makeEntry := range map[string]func(t *testing.T, path string){
		"a fifo": func(t *testing.T, path string) {
			t.Helper()

			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Skipf("mkfifo unsupported here: %v", err)
			}
		},
		"a symlink": func(t *testing.T, path string) {
			t.Helper()

			target := filepath.Join(filepath.Dir(path), "real.txt")
			if err := os.WriteFile(target, []byte("real"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			artifactDir := fsys.MkdirAll("dist-root")

			makeEntry(t, filepath.Join(artifactDir, "demo.jar"))

			cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\n"))

			err := appcontainer.ValidateArtifacts(io.Discard, output.Annotator{},
				appcontainer.ValidateArtifactsInput{
					ProjectType: "maven", ArtifactDir: artifactDir, ContainerfilePath: cf,
				})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation for %s", err, name)
			}

			if !strings.Contains(err.Error(), "regular file") {
				t.Errorf("err = %v, want it to name the regular-file rule", err)
			}
		})
	}
}
