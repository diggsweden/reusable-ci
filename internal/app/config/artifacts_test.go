// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	appconfig "github.com/diggsweden/reusable-ci/internal/app/config"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
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

func configPlanFromSink(t *testing.T, sink *fakeoutputsink.Sink) pipeline.ConfigPlan {
	t.Helper()

	raw := sink.Single("config-plan-json")
	if raw == "" {
		t.Fatal("config-plan-json output was not emitted")
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}

	return plan
}

func requireOnlyConfigPlanOutput(t *testing.T, sink *fakeoutputsink.Sink) {
	t.Helper()

	keys := sink.Keys()
	if len(keys) != 1 || keys[0] != "config-plan-json" {
		t.Fatalf("unexpected outputs: %v", keys)
	}
}

func TestEmitConfigPlan_HappyPath(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-app
    project-type: maven
    build-type: library
    publish-to: [maven-central]
    require-authorization: true
`)
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}

	err := appconfig.EmitConfigPlan(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.Maven) != 1 || plan.Artifacts.Maven[0].Name != "my-app" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("maven artifacts = %+v", plan.Artifacts.Maven)
	}

	if len(plan.Artifacts.MavenCentral) != 1 || plan.Artifacts.MavenCentral[0].Name != "my-app" {
		t.Errorf("maven central artifacts = %+v", plan.Artifacts.MavenCentral)
	}

	if !plan.AnyRequireAuthorization {
		t.Error("any_require_authorization should be true")
	}

	if plan.PipelineSBOMs != "build,analyzed-artifact,analyzed-container" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("pipeline_sboms = %q", plan.PipelineSBOMs)
	}

	requireOnlyConfigPlanOutput(t, sink)
	// Summary block.
	if !strings.Contains(summary.buf.String(), "### my-app") {
		t.Errorf("summary missing artifact block: %s", summary.buf.String())
	}
}

func TestEmitConfigPlan_ReadsFromFS(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewMemory(t)
	fsys.WriteFile("config/artifacts.yml", []byte(`
artifacts:
  - name: mem-app
    project-type: npm
`))

	sink := fakeoutputsink.New(t)

	err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{
		Path: "config/artifacts.yml",
		FS:   fsys.FS(),
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.NPM) != 1 || plan.Artifacts.NPM[0].Name != "mem-app" {
		t.Errorf("npm artifacts = %+v", plan.Artifacts.NPM)
	}
}

func TestEmitConfigPlan_EmptyArtifactsErrors(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, "artifacts: []\n")
	sink := fakeoutputsink.New(t)

	err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path})
	if err == nil || !strings.Contains(err.Error(), "no artifacts found") {
		t.Errorf("err = %v", err)
	}
}

func TestEmitConfigPlan_FileNotFoundFallsToAutoDeriveAndReportsMissingManifest(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	// Empty in-memory filesystem — no artifacts.yml AND no manifest.
	// Auto-derive must surface the actionable "no recognised manifest"
	// message rather than the older raw "file not found".
	empty := fstest.MapFS{}

	err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{
		Path: ".reusable-ci/artifacts.yml",
		FS:   empty,
	})
	if err == nil || !strings.Contains(err.Error(), "no recognised manifest at repo root") {
		t.Errorf("expected actionable auto-derive failure, got: %v", err)
	}
}

func TestEmitConfigPlan_MavenAppToGitHubPackagesExcluded(t *testing.T) {
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

	err := appconfig.EmitConfigPlan(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.GitHubPackages) != 1 || plan.Artifacts.GitHubPackages[0].Name != "lib" {
		t.Errorf("library should be in github_packages: %+v", plan.Artifacts.GitHubPackages)
	}

	for _, artifact := range plan.Artifacts.GitHubPackages {
		if artifact.Name == "app" {
			t.Errorf("maven application should be excluded from github_packages: %+v", plan.Artifacts.GitHubPackages)
		}
	}
}

func TestEmitConfigPlan_ContainersDeriveTypesAndAnalyzedSBOM(t *testing.T) {
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

	err := appconfig.EmitConfigPlan(context.Background(), sink, summary, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path})
	if err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Containers.All) != 1 {
		t.Fatalf("got %d containers", len(plan.Containers.All))
	}

	c := plan.Containers.All[0] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if c.Name != "my-app" {
		t.Errorf("name = %v", c.Name)
	}

	if got := c.EnableAnalyzedContainerSBOM; got != true {
		t.Errorf("enable-analyzed-container-sbom = %v", got)
	}

	if got := c.BuildArgsString; got != "BAZ=qux\nFOO=bar" {
		t.Errorf("build-args-string = %v", got)
	}
}

func TestEmitConfigPlan_AuthorizationFalseWhenNoneRequire(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: foo
    project-type: maven
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if plan.AnyRequireAuthorization {
		t.Error("any_require_authorization should be false")
	}
}

func TestEmitConfigPlan_PipelineSBOMsNoneForMetaOnlyConfig(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: meta
    project-type: meta
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if plan.PipelineSBOMs != "none" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("pipeline_sboms = %q (meta defaults to none)", plan.PipelineSBOMs)
	}
}

//nolint:cyclop // checks every per-artifact output on one plan.
func TestEmitConfigPlan_ProjectTypeAndPublishTargetOutputs(t *testing.T) {
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
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.NPM) != 1 || plan.Artifacts.NPM[0].Name != "my-npm-lib" {
		t.Errorf("npm artifacts = %+v", plan.Artifacts.NPM)
	}

	if len(plan.Artifacts.GitHubPackages) != 1 || plan.Artifacts.GitHubPackages[0].Name != "my-npm-lib" {
		t.Errorf("github packages artifacts = %+v", plan.Artifacts.GitHubPackages)
	}

	if len(plan.Artifacts.GradleAndroid) != 1 || plan.Artifacts.GradleAndroid[0].Name != "android-app" {
		t.Errorf("gradle android artifacts = %+v", plan.Artifacts.GradleAndroid)
	}

	if len(plan.Artifacts.GooglePlay) != 1 || plan.Artifacts.GooglePlay[0].Name != "android-app" {
		t.Errorf("google play artifacts = %+v", plan.Artifacts.GooglePlay)
	}

	if plan.PipelineSBOMs != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("pipeline_sboms = %q", plan.PipelineSBOMs)
	}
}

func TestEmitConfigPlan_GoBuildModeOutputs(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: go-cli
    project-type: go
    config:
      build-mode: artifact-first
  - name: go-service
    project-type: go
    config:
      build-mode: container-first
containers:
  - name: go-service
    from: [go-cli, go-service]
    container-file: Containerfile
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.Go) != 2 {
		t.Errorf("go artifacts = %+v", plan.Artifacts.Go)
	}

	if len(plan.Artifacts.GoArtifactFirst) != 1 || plan.Artifacts.GoArtifactFirst[0].Name != "go-cli" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("go artifact first = %+v", plan.Artifacts.GoArtifactFirst)
	}

	if len(plan.Artifacts.GoContainerFirst) != 1 || plan.Artifacts.GoContainerFirst[0].Name != "go-service" {
		t.Errorf("go container first = %+v", plan.Artifacts.GoContainerFirst)
	}

	if got := plan.Containers.All[0].GoArtifactName; got != "go-cli" {
		t.Errorf("go-artifact-name = %v", got)
	}
}

// TestEmitConfigPlan_CargoBuildModeOutputs is the Cargo counterpart of
// TestEmitConfigPlan_GoBuildModeOutputs — it confirms config parse
// emits artefact-first vs container-first cargo splits and surfaces the
// upload name on the consuming container. Same shape, same exit codes,
// same data flow.
func TestEmitConfigPlan_CargoBuildModeOutputs(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: rust-cli
    project-type: cargo
    config:
      build-mode: artifact-first
  - name: rust-service
    project-type: cargo
    config:
      build-mode: container-first
containers:
  - name: rust-service
    from: [rust-cli, rust-service]
    container-file: Containerfile
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.Cargo) != 2 {
		t.Errorf("cargo artifacts = %+v", plan.Artifacts.Cargo)
	}

	if len(plan.Artifacts.CargoArtifactFirst) != 1 || plan.Artifacts.CargoArtifactFirst[0].Name != "rust-cli" {
		t.Errorf("cargo artifact first = %+v", plan.Artifacts.CargoArtifactFirst)
	}

	if len(plan.Artifacts.CargoContainerFirst) != 1 || plan.Artifacts.CargoContainerFirst[0].Name != "rust-service" {
		t.Errorf("cargo container first = %+v", plan.Artifacts.CargoContainerFirst)
	}

	if got := plan.Containers.All[0].CargoArtifactName; got != "rust-cli" {
		t.Errorf("cargo-artifact-name = %v", got)
	}
}

//nolint:cyclop // verifies all top-level fields of the emitted ConfigPlan JSON on one fixture.
func TestEmitConfigPlan_EmitsConfigPlanJSON(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-lib
    project-type: maven
    build-type: library
    publish-to: [maven-central]
    require-authorization: true
  - name: go-cli
    project-type: go
    config:
      build-mode: artifact-first
  - name: go-service
    project-type: go
    working-directory: services/go-service
    config:
      build-mode: container-first
containers:
  - name: go-service
    from: [go-cli, go-service]
    container-file: Containerfile
    build-args:
      SERVICE: go-service
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	raw := sink.Single("config-plan-json")
	if raw == "" {
		t.Fatal("config-plan-json output was not emitted")
	}

	for _, legacyName := range []string{"project-type", "working-directory", "effective-sboms", "go-artifact-name", "cargo-artifact-name"} {
		if strings.Contains(raw, `"`+legacyName+`"`) {
			t.Fatalf("config-plan-json should not contain legacy field %q: %s", legacyName, raw)
		}
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.Version != pipeline.ConfigPlanVersion {
		t.Errorf("version = %d, want %d", plan.Version, pipeline.ConfigPlanVersion)
	}

	if plan.FallbackProjectType != projecttype.Maven {
		t.Errorf("fallback_project_type = %q", plan.FallbackProjectType)
	}

	if !plan.AnyRequireAuthorization {
		t.Error("any_require_authorization should be true")
	}

	if plan.PipelineSBOMs != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("pipeline_sboms = %q", plan.PipelineSBOMs)
	}

	if len(plan.Artifacts.MavenCentral) != 1 || plan.Artifacts.MavenCentral[0].Name != "my-lib" {
		t.Errorf("maven_central artifacts = %+v", plan.Artifacts.MavenCentral)
	}

	if len(plan.Artifacts.GoArtifactFirst) != 1 || plan.Artifacts.GoArtifactFirst[0].Name != "go-cli" {
		t.Errorf("go_artifact_first = %+v", plan.Artifacts.GoArtifactFirst)
	}

	if got := plan.Artifacts.GoArtifactFirst[0].WorkingDirectory; got != "." {
		t.Errorf("go-cli working_directory = %q", got)
	}

	if len(plan.Artifacts.GoContainerFirst) != 1 || plan.Artifacts.GoContainerFirst[0].Name != "go-service" {
		t.Errorf("go_container_first = %+v", plan.Artifacts.GoContainerFirst)
	}

	if got := plan.Artifacts.GoContainerFirst[0].WorkingDirectory; got != "services/go-service" {
		t.Errorf("go-service working_directory = %q", got)
	}

	if !plan.Containers.HasContainers || len(plan.Containers.All) != 1 {
		t.Fatalf("containers = %+v", plan.Containers)
	}

	container := plan.Containers.All[0]
	if container.ContainerFile != "Containerfile" || container.Context != "." || container.Platforms != "linux/amd64" {
		t.Errorf("container defaults = %+v", container)
	}

	if container.GoArtifactName != "go-cli" {
		t.Errorf("go_artifact_name = %q", container.GoArtifactName)
	}

	if got := container.BuildArgsString; got != "SERVICE=go-service" {
		t.Errorf("build_args_string = %q", got)
	}

	requireOnlyConfigPlanOutput(t, sink)
}

func TestEmitConfigPlan_GooglePlayArtifactsEmptyWhenUnused(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: my-app
    project-type: maven
    build-type: library
    working-directory: .
    publish-to: [maven-central]
`)

	sink := fakeoutputsink.New(t)
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.GooglePlay) != 0 {
		t.Errorf("google_play artifacts = %+v", plan.Artifacts.GooglePlay)
	}
}

func TestEmitConfigPlan_MultiplePublishTargets(t *testing.T) {
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
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Artifacts.MavenCentral) != 1 || plan.Artifacts.MavenCentral[0].Name != "multi-publish-lib" {
		t.Errorf("maven central artifacts = %+v", plan.Artifacts.MavenCentral)
	}

	if len(plan.Artifacts.GitHubPackages) != 1 || plan.Artifacts.GitHubPackages[0].Name != "multi-publish-lib" {
		t.Errorf("github packages artifacts = %+v", plan.Artifacts.GitHubPackages)
	}
}

func TestEmitConfigPlan_ContainerTargetExtractAndBuildArgsArePreserved(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
artifacts:
  - name: hsm-worker
    project-type: cargo
    working-directory: hsm-worker
    config:
      build-mode: container-first
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
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Containers.All) != 1 {
		t.Fatalf("got %d containers", len(plan.Containers.All))
	}

	c := plan.Containers.All[0] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if c.Target != "runtime" {
		t.Errorf("target = %v", c.Target)
	}

	if c.ExtractBinaryTarget != "export-binary" {
		t.Errorf("extract_binary_target = %v", c.ExtractBinaryTarget)
	}

	if len(c.ExtractBinaryNames) != 2 || c.ExtractBinaryNames[0] != "hsm-worker" || c.ExtractBinaryNames[1] != "digg-hsm-keytool" {
		t.Fatalf("extract_binary_names = %v", c.ExtractBinaryNames)
	}

	if got := c.BuildArgsString; got != "RUST_VERSION=1.94\nSERVICE=hsm-worker" {
		t.Errorf("build-args-string = %v", got)
	}
}

func TestEmitConfigPlan_ContainerDefaultsEmitFalseAndEmptyDerivedFields(t *testing.T) {
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
	if err := appconfig.EmitConfigPlan(context.Background(), sink, nil, &bytes.Buffer{}, output.Annotator{}, appconfig.EmitConfigPlanInput{Path: path}); err != nil {
		t.Fatal(err)
	}

	plan := configPlanFromSink(t, sink)
	if len(plan.Containers.All) != 1 {
		t.Fatalf("got %d containers", len(plan.Containers.All))
	}

	c := plan.Containers.All[0]
	if got := c.EnableAnalyzedContainerSBOM; got != false {
		t.Errorf("enable-analyzed-container-sbom = %v", got)
	}

	if got := c.BuildArgsString; got != "" {
		t.Errorf("build-args-string = %v", got)
	}
}
