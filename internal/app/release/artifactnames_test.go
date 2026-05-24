// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestResolveArtifactNames_PrintsOutputPairs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		projType, artifact, wantName, wantSBOM string
	}{
		{"maven", "", "maven-build-artifacts", "maven-build-sbom"},
		{"npm", "web", "web-build-artifacts", "web-build-sbom"},
		{"gradle", "my-component", "my-component-build-artifacts", "my-component-build-sbom"},
		{"python", "", "python-build-artifacts", "python-build-sbom"},
		{"go", "", "go-build-artifacts", "go-build-sbom"},
		{"cargo", "core", "core-cargo-build-artifacts", "core-cargo-build-sbom"},
		{"unknown", "", "build-artifacts", "build-sbom"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"Maven", "", "build-artifacts", "build-sbom"},
		{"NPM", "", "build-artifacts", "build-sbom"},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
	if err == nil || !strings.Contains(err.Error(), "project-type is required") {
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

func TestResolveArtifactNames_GitHubFormatEmitsOutputs(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := apprelease.ResolveArtifactNames(context.Background(), &out, apprelease.ResolveArtifactNamesInput{
		ProjectType:  "go",
		ArtifactName: "cli",
		Format:       output.FormatGitHub,
		Sink:         sink,
	}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("name"); got != "cli-go-build-artifacts" {
		t.Errorf("name = %q", got)
	}

	if got := sink.Single("sbom-name"); got != "cli-go-build-sbom" {
		t.Errorf("sbom-name = %q", got)
	}

	if !strings.Contains(out.String(), "name=cli-go-build-artifacts") {
		t.Errorf("out = %q", out.String())
	}
}

func TestResolveMetadata_OverrideName(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var stderr bytes.Buffer

	err := apprelease.ResolveMetadata(context.Background(), sink, &stderr, apprelease.ResolveMetadataInput{
		Version: "v1.2.3", Repository: "owner/repo", ArtifactName: "custom", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
		Version: "1.0.0", Repository: "group/sub/myproject", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
	}); err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Errorf("missing version err = %v", err)
	}

	if err := apprelease.ResolveMetadata(context.Background(), sink, &bytes.Buffer{}, apprelease.ResolveMetadataInput{
		Version: "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err == nil || !strings.Contains(err.Error(), "repository is required") {
		t.Errorf("missing repository err = %v", err)
	}
}
