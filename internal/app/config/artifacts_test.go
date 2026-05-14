// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	appconfig "github.com/diggsweden/reusable-ci/internal/app/config"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeSummary struct{ buf bytes.Buffer }

func (f *fakeSummary) Append(_ context.Context, s string) error {
	f.buf.WriteString(s)
	return nil
}

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	return testfs.NewReal(t).WriteFile("artifacts.yml", []byte(body))
}

func TestParseArtifacts_HappyPath(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-app
    project-type: maven
    publish-to: [maven-central]
    require-authorization: true
`)
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}
	err := appconfig.ParseArtifacts(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	// Per-type outputs.
	if got := sink.Single("maven-artifacts"); !strings.Contains(got, `"my-app"`) {
		t.Errorf("maven-artifacts = %q", got)
	}
	if got := sink.Single("npm-artifacts"); got != "[]" {
		t.Errorf("npm-artifacts should be empty array: %q", got)
	}
	if got := sink.Single("gradleandroid-artifacts"); got != "[]" {
		t.Errorf("gradle-android key should normalise to gradleandroid: keys=%v", sink.Keys())
	}
	// Per-target outputs.
	if got := sink.Single("mavencentral-artifacts"); !strings.Contains(got, `"my-app"`) {
		t.Errorf("mavencentral-artifacts = %q", got)
	}
	// Authorization flag.
	if got := sink.Single("any-require-authorization"); got != "true" {
		t.Errorf("any-require-authorization = %q", got)
	}
	// SBOMs default = "all" for maven, so pipeline-sboms = full set.
	if got := sink.Single("pipeline-sboms"); got != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("pipeline-sboms = %q", got)
	}
	// Summary block.
	if !strings.Contains(summary.buf.String(), "### my-app") {
		t.Errorf("summary missing artifact block: %s", summary.buf.String())
	}
}

func TestParseArtifacts_ReadsFromFS(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewMemory(t)
	fsys.WriteFile("config/artifacts.yml", []byte(`
artifacts:
  - name: mem-app
    project-type: npm
`))
	sink := fakeoutputsink.New(t)

	err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{
		Path: "config/artifacts.yml",
		FS:   fsys.FS(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("npm-artifacts"); !strings.Contains(got, `"mem-app"`) {
		t.Errorf("npm-artifacts = %q", got)
	}
}

func TestParseArtifacts_EmptyArtifactsErrors(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, "artifacts: []\n")
	sink := fakeoutputsink.New(t)
	err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path})
	if err == nil || !strings.Contains(err.Error(), "No artifacts found") {
		t.Errorf("err = %v", err)
	}
}

func TestParseArtifacts_FileNotFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: "/nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "File not found") {
		t.Errorf("err = %v", err)
	}
}

func TestParseArtifacts_MavenAppToGitHubPackagesExcluded(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: app
    project-type: maven
    build-type: application
    publish-to: [github-packages]
  - name: lib
    project-type: maven
    build-type: library
    publish-to: [github-packages]
`)
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}
	err := appconfig.ParseArtifacts(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.Single("githubpackages-artifacts")
	if !strings.Contains(body, `"lib"`) {
		t.Errorf("library should be in githubpackages: %s", body)
	}
	if strings.Contains(body, `"app"`) {
		t.Errorf("maven application should be excluded from githubpackages: %s", body)
	}
}

func TestParseArtifacts_ContainersDeriveTypesAndAnalyzedSBOM(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: backend
    project-type: maven
  - name: frontend
    project-type: npm
    sboms: "all"
containers:
  - name: my-app
    from: [backend, frontend]
    container-file: Containerfile
    build-args:
      FOO: bar
      BAZ: qux
`)
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}
	err := appconfig.ParseArtifacts(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	containersJSON := sink.Single("containers")
	var containers []map[string]any
	if err := json.Unmarshal([]byte(containersJSON), &containers); err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("got %d containers", len(containers))
	}
	c := containers[0]
	if c["name"] != "my-app" {
		t.Errorf("name = %v", c["name"])
	}
	if got := c["enable-analyzed-container-sbom"]; got != true {
		t.Errorf("enable-analyzed-container-sbom = %v", got)
	}
	if got := c["build-args-string"]; got != "BAZ=qux\nFOO=bar" {
		t.Errorf("build-args-string = %v", got)
	}
}

func TestParseArtifacts_AuthorizationFalseWhenNoneRequire(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: foo
    project-type: maven
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("any-require-authorization"); got != "false" {
		t.Errorf("any-require-authorization = %q", got)
	}
}

func TestParseArtifacts_PipelineSBOMsNoneForMetaOnlyConfig(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: meta
    project-type: meta
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("pipeline-sboms"); got != "none" {
		t.Errorf("pipeline-sboms = %q (meta defaults to none)", got)
	}
}

func TestParseArtifacts_ProjectTypeAndPublishTargetOutputs(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-npm-lib
    project-type: npm
    build-type: library
    working-directory: packages/lib
    publish-to: [github-packages]
  - name: android-app
    project-type: gradle-android
    build-type: application
    working-directory: app
    publish-to: [google-play]
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("npm-artifacts"); !strings.Contains(got, `"my-npm-lib"`) {
		t.Errorf("npm-artifacts = %q", got)
	}
	if got := sink.Single("githubpackages-artifacts"); !strings.Contains(got, `"my-npm-lib"`) {
		t.Errorf("githubpackages-artifacts = %q", got)
	}
	if got := sink.Single("gradleandroid-artifacts"); !strings.Contains(got, `"android-app"`) {
		t.Errorf("gradleandroid-artifacts = %q", got)
	}
	if got := sink.Single("googleplay-artifacts"); !strings.Contains(got, `"android-app"`) {
		t.Errorf("googleplay-artifacts = %q", got)
	}
	if got := sink.Single("pipeline-sboms"); got != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("pipeline-sboms = %q", got)
	}
}

func TestParseArtifacts_GooglePlayArtifactsEmptyWhenUnused(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-app
    project-type: maven
    build-type: application
    working-directory: .
    publish-to: [maven-central]
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("googleplay-artifacts"); got != "[]" {
		t.Errorf("googleplay-artifacts = %q", got)
	}
}

func TestParseArtifacts_MultiplePublishTargets(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: multi-publish-lib
    project-type: maven
    build-type: library
    working-directory: .
    publish-to: [maven-central, github-packages]
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mavencentral-artifacts", "githubpackages-artifacts"} {
		if got := sink.Single(key); !strings.Contains(got, `"multi-publish-lib"`) {
			t.Errorf("%s = %q", key, got)
		}
	}
}

func TestParseArtifacts_ContainerTargetExtractAndBuildArgsArePreserved(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: hsm-worker
    project-type: cargo
    working-directory: hsm-worker
containers:
  - name: hsm-worker
    from: [hsm-worker]
    container-file: hsm-worker/Containerfile
    context: .
    target: runtime
    extract:
      binary:
        target: export-binary
        names: [hsm-worker, digg-hsm-keytool]
    build-args:
      SERVICE: hsm-worker
      RUST_VERSION: "1.94"
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	var containers []map[string]any
	if err := json.Unmarshal([]byte(sink.Single("containers")), &containers); err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("got %d containers", len(containers))
	}
	c := containers[0]
	if c["target"] != "runtime" {
		t.Errorf("target = %v", c["target"])
	}
	extract, ok := c["extract"].(map[string]any)
	if !ok {
		t.Fatalf("extract = %T %v", c["extract"], c["extract"])
	}
	binary, ok := extract["binary"].(map[string]any)
	if !ok {
		t.Fatalf("extract.binary = %T %v", extract["binary"], extract["binary"])
	}
	if binary["target"] != "export-binary" {
		t.Errorf("extract.binary.target = %v", binary["target"])
	}
	names, ok := binary["names"].([]any)
	if !ok || len(names) != 2 {
		t.Fatalf("extract.binary.names = %T %v", binary["names"], binary["names"])
	}
	if got := c["build-args-string"]; got != "RUST_VERSION=1.94\nSERVICE=hsm-worker" {
		t.Errorf("build-args-string = %v", got)
	}
}

func TestParseArtifacts_ContainerDefaultsEmitFalseAndEmptyDerivedFields(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: api
    project-type: maven
    working-directory: .
containers:
  - name: standalone-image
    from: []
    container-file: Containerfile
`)
	sink := fakeoutputsink.New(t)
	if err := appconfig.ParseArtifacts(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.ParseArtifactsInput{Path: path}); err != nil {
		t.Fatal(err)
	}
	var containers []map[string]any
	if err := json.Unmarshal([]byte(sink.Single("containers")), &containers); err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("got %d containers", len(containers))
	}
	c := containers[0]
	if got := c["enable-analyzed-container-sbom"]; got != false {
		t.Errorf("enable-analyzed-container-sbom = %v", got)
	}
	if got := c["build-args-string"]; got != "" {
		t.Errorf("build-args-string = %v", got)
	}
}
