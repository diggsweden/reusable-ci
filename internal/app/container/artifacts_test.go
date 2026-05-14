// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container_test

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestValidateArtifacts_MavenFindsJARs(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("demo-1.0.0.jar", []byte("fake-jar"))
	cf := fsys.WriteFile("Containerfile", []byte("FROM alpine\nCOPY demo-1.0.0.jar /app/\n"))
	var stdout bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&stdout, io.Discard, output.Annotator{}, appcontainer.ValidateArtifactsInput{
		ProjectType: "maven", ArtifactDir: fsys.Root, ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "✓ Maven artifacts found:") {
		t.Errorf("missing success header:\n%s", stdout.String())
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

func TestValidateArtifacts_MissingArtifactsWarn(t *testing.T) {
	tests := []struct {
		name        string
		projectType string
		wantStdout  []string
		wantStderr  []string
	}{
		{
			name:        "npm",
			projectType: "npm",
			wantStdout:  []string{"Container build may fail"},
			wantStderr:  []string{"::warning::No NPM artifacts found"},
		},
		{
			name:        "maven",
			projectType: "maven",
			wantStdout:  []string{"acceptable if container builds from source", "Container build may fail"},
			wantStderr:  []string{"::warning::"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			var stdout, stderr bytes.Buffer
			if err := appcontainer.ValidateArtifacts(&stdout, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
				ProjectType: testCase.projectType,
				ArtifactDir: fsys.Root,
			}); err != nil {
				t.Fatal(err)
			}
			for _, want := range testCase.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("missing %q in stdout:\n%s", want, stdout.String())
				}
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
	var stdout, stderr bytes.Buffer
	if err := appcontainer.ValidateArtifacts(&stdout, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appcontainer.ValidateArtifactsInput{
		ProjectType:       "maven",
		ArtifactDir:       fsys.Root,
		ContainerfilePath: cf,
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"::warning::Containerfile rebuilds from source", "downloaded artifacts may be ignored", "COPY pre-built artifacts", "Dockerfile"} {
		if !strings.Contains(stdout.String(), want) && !strings.Contains(stderr.String(), want) {
			t.Errorf("missing %q\nstdout:\n%s\nstderr:\n%s", want, stdout.String(), stderr.String())
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
