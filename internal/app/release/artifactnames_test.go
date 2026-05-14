// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestResolveArtifactNames_PrintsOutputPairs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		projType, artifact, wantName, wantSBOM string
	}{
		{"maven", "", "maven-build-artifacts", "maven-build-sbom"},
		{"npm", "ignored", "npm-build-artifacts", "npm-build-sbom"},
		{"gradle", "my-component", "my-component", "my-component-sbom"},
		{"python", "", "python-build-artifacts", "python-build-sbom"},
		{"go", "", "go-build-artifacts", "go-build-sbom"},
		{"cargo", "core", "core-cargo-build-sbom", "core-cargo-build-sbom"},
		{"unknown", "", "build-artifacts", "build-sbom"},
		{"Maven", "", "build-artifacts", "build-sbom"},
		{"NPM", "", "build-artifacts", "build-sbom"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := apprelease.ResolveArtifactNames(context.Background(), &buf, apprelease.ResolveArtifactNamesInput{
			ProjectType: c.projType, ArtifactName: c.artifact,
		}); err != nil {
			t.Fatal(err)
		}
		want := "name=" + c.wantName + "\nsbom-name=" + c.wantSBOM + "\n"
		if buf.String() != want {
			t.Errorf("project=%s artifact=%q\n got  %q\n want %q", c.projType, c.artifact, buf.String(), want)
		}
	}
}

func TestResolveArtifactNames_EmptyProjectErrors(t *testing.T) {
	t.Parallel()
	err := apprelease.ResolveArtifactNames(context.Background(), &bytes.Buffer{}, apprelease.ResolveArtifactNamesInput{})
	if err == nil || !strings.Contains(err.Error(), "Usage") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveArtifactNames_ExactTwoLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := apprelease.ResolveArtifactNames(context.Background(), &buf, apprelease.ResolveArtifactNamesInput{
		ProjectType: "maven",
	}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "name=") || !strings.HasPrefix(lines[1], "sbom-name=") {
		t.Errorf("unexpected output lines: %v", lines)
	}
}

func TestResolveMetadata_OverrideName(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	var stderr bytes.Buffer
	err := apprelease.ResolveMetadata(context.Background(), sink, &stderr, apprelease.ResolveMetadataInput{
		Version: "v1.2.3", Repository: "owner/repo", ArtifactName: "custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("project-name"); got != "custom" {
		t.Errorf("project-name = %q", got)
	}
	if got := sink.Single("version"); got != "v1.2.3" {
		t.Errorf("version = %q", got)
	}
	if got := sink.Single("version-no-v"); got != "1.2.3" {
		t.Errorf("version-no-v = %q", got)
	}
	if !strings.Contains(stderr.String(), "Using artifact name: custom") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestResolveMetadata_DerivesFromRepo(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	var stderr bytes.Buffer
	err := apprelease.ResolveMetadata(context.Background(), sink, &stderr, apprelease.ResolveMetadataInput{
		Version: "1.0.0", Repository: "group/sub/myproject",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("project-name"); got != "myproject" {
		t.Errorf("project-name = %q", got)
	}
	if got := sink.Single("version-no-v"); got != "1.0.0" {
		t.Errorf("version-no-v = %q (no leading v to strip)", got)
	}
	if !strings.Contains(stderr.String(), "Using repository name: myproject") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestResolveMetadata_RequiresVersionAndRepo(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := apprelease.ResolveMetadata(context.Background(), sink, &bytes.Buffer{}, apprelease.ResolveMetadataInput{
		Repository: "owner/repo",
	}); err == nil || !strings.Contains(err.Error(), "VERSION is required") {
		t.Errorf("missing VERSION err = %v", err)
	}
	if err := apprelease.ResolveMetadata(context.Background(), sink, &bytes.Buffer{}, apprelease.ResolveMetadataInput{
		Version: "v1.0.0",
	}); err == nil || !strings.Contains(err.Error(), "REPOSITORY is required") {
		t.Errorf("missing REPOSITORY err = %v", err)
	}
}
