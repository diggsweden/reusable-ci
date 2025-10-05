// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestConfigPlanBoundary_RejectsBeforeEveryOutput(t *testing.T) {
	t.Parallel()

	raw := producedBoundaryConfigPlan(t)
	for _, tc := range []struct {
		name, reason string
		mutate       func(*pipeline.ConfigPlan)
	}{
		{"missing build membership", "artifacts.maven disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.Maven = nil }},
		{"duplicate publish membership", "artifacts.maven_central disagrees", func(p *pipeline.ConfigPlan) {
			p.Artifacts.MavenCentral = append(p.Artifacts.MavenCentral, p.Artifacts.MavenCentral[0])
		}},
		{"late contradictory copy", "artifacts.maven_central disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.MavenCentral[0].WorkingDirectory = "other-project" }},
		{"late nested copy", "artifacts.maven_central disagrees", func(p *pipeline.ConfigPlan) { p.Artifacts.MavenCentral[0].Maven.SettingsPath = "other-settings.xml" }},
		{"duplicate all", "artifacts.all[2]", func(p *pipeline.ConfigPlan) {
			p.Artifacts.All = append(p.Artifacts.All, p.Artifacts.All[1])
			p.Artifacts.Maven = append(p.Artifacts.Maven, p.Artifacts.Maven[0])
			p.Artifacts.MavenCentral = append(p.Artifacts.MavenCentral, p.Artifacts.MavenCentral[0])
		}},
		{"authorization suppression", "any_require_authorization", func(p *pipeline.ConfigPlan) { p.AnyRequireAuthorization = false }},
		{"sbom suppression", "pipeline_sboms disagrees", func(p *pipeline.ConfigPlan) { p.PipelineSBOMs = "none" }},
		{"container suppression", "containers.has_containers", func(p *pipeline.ConfigPlan) { p.Containers.HasContainers = false }},
		{"duplicate container", "containers.all[1]", func(p *pipeline.ConfigPlan) { p.Containers.All = append(p.Containers.All, p.Containers.All[0]) }},
		{"fallback despite override", "fallback_project_type", func(p *pipeline.ConfigPlan) { p.FallbackProjectType = "go" }},
		{"oidc gate", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.RequiresIDToken = false }},
		{"gpg gate", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.ImportsGPGKey = true }},
		{"container signing gate", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.SignsContainers = false }},
		{"egress gate", "sign has inconsistent", func(p *pipeline.ConfigPlan) { p.Sign.RequiresSigstoreEgress = false }},
		{"git signing gate", "git_signing has inconsistent", func(p *pipeline.ConfigPlan) { p.GitSigning.ImportsGPGKey = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var plan pipeline.ConfigPlan
			if err := json.Unmarshal([]byte(raw), &plan); err != nil {
				t.Fatal(err)
			}

			tc.mutate(&plan)
			assertConfigPlanBoundaryRefusal(t, mustConfigPlanJSON(t, plan), tc.reason)
		})
	}

	for _, tc := range []struct{ name, raw, reason string }{
		{"malformed", raw[:len(raw)-1], "parse config-plan-json"},
		{"null", "null", "unsupported config-plan version"},
		{"array", "[]", "parse config-plan-json"},
		{"scalar", "true", "parse config-plan-json"},
		{"missing envelope", `{"version":1}`, "pipeline_sboms"},
		{"future version", strings.Replace(raw, `"version":1`, `"version":2`, 1), "unsupported config-plan version 2"},
		{"wrong group shape", strings.Replace(raw, `"maven":[`, `"maven":true,"ignored":[`, 1), "parse config-plan-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertConfigPlanBoundaryRefusal(t, tc.raw, tc.reason)
		})
	}
}

func TestConfigPlanBoundary_ProducerControl(t *testing.T) {
	t.Parallel()
	raw := producedBoundaryConfigPlan(t)
	// Wire v1 remains supported; additional fields are not a schema-equality error.
	raw = strings.TrimSuffix(raw, "}") + `,"producer_note":"optional extension"}`
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}

	release, err := appplan.Release(t.Context(), sink, summary, appplan.ReleaseInput{
		ConfigPlanJSON: raw, ReleaseSBOMs: "all", ReleaseSignArtifacts: true, ReleasePublisher: "github-cli", RefName: "v1.2.3",
	})
	if err != nil || release == nil {
		t.Fatalf("release producer control: %v", err)
	}

	if !release.Policy.RequireAllowlistedSigner || !release.Policy.HasContainers || release.Stages.Prepare.Targets.VersionBump.Runs || len(release.Stages.Prepare.Targets.VersionBump.Items) != 2 {
		t.Fatalf("release policy or disabled items changed: %+v", release)
	}

	assertBoundaryOutputs(t, sink, []string{"release-plan-json", "prepare-stage-plan-json", "build-stage-plan-json", "publish-stage-plan-json", "artifact-transfer-plan-json"}, []any{release, release.Stages.Prepare, release.Stages.Build, release.Stages.Publish, release.ArtifactTransfers})

	if summary.buf.Len() != 0 {
		t.Fatalf("unexpected summary: %q", summary.buf.String())
	}

	for _, publishNPM := range []bool{false, true} {
		snapshotSink := fakeoutputsink.New(t)

		snapshot, snapshotErr := appplan.SnapshotRelease(t.Context(), snapshotSink, appplan.SnapshotReleaseInput{ConfigPlanJSON: raw, ProjectType: "maven", SBOMs: "none", PublishNPM: publishNPM})
		if snapshotErr != nil || snapshot == nil {
			t.Fatalf("snapshot producer control: %v", snapshotErr)
		}

		if snapshot.Context.ProjectType != "maven" || snapshot.Stages.Publish.Targets.NPM.Runs != publishNPM || len(snapshot.Stages.Publish.Targets.NPM.Items) != 1 {
			t.Fatalf("override or disabled items changed: %+v", snapshot)
		}

		assertBoundaryOutputs(t, snapshotSink, []string{"snapshot-release-plan-json", "snapshot-build-stage-plan-json", "snapshot-publish-stage-plan-json", "artifact-transfer-plan-json"}, []any{snapshot, snapshot.Stages.Build, snapshot.Stages.Publish, snapshot.ArtifactTransfers})
	}
}

func assertConfigPlanBoundaryRefusal(t *testing.T, raw, reason string) {
	t.Helper()

	var events []string

	sink := &patternSink{Sink: fakeoutputsink.New(t), events: &events}
	summary := &fakeSummary{}

	release, err := appplan.Release(t.Context(), sink, summary, appplan.ReleaseInput{ConfigPlanJSON: raw, ReleaseSBOMs: "all", ReleaseSignArtifacts: true, ReleasePublisher: "github-cli", RefName: "v1.2.3"})
	if release != nil || !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), reason) {
		t.Errorf("release = %+v, %v, want nil / %s / ErrInvalidConfig", release, err, reason)
	}

	if len(events) != 0 || len(sink.Keys()) != 0 || summary.buf.Len() != 0 {
		t.Errorf("release refusal published: calls=%v outputs=%v summary=%q", events, sink.AllScalar(), summary.buf.String())
	}

	snapshot, err := appplan.SnapshotRelease(t.Context(), sink, appplan.SnapshotReleaseInput{ConfigPlanJSON: raw, ProjectType: "maven", SBOMs: "none", PublishNPM: true})
	if snapshot != nil || !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), reason) {
		t.Errorf("snapshot = %+v, %v, want nil / %s / ErrInvalidConfig", snapshot, err, reason)
	}

	if len(events) != 0 || len(sink.Keys()) != 0 || summary.buf.Len() != 0 {
		t.Errorf("snapshot refusal published: calls=%v outputs=%v summary=%q", events, sink.AllScalar(), summary.buf.String())
	}
}

func assertBoundaryOutputs(t *testing.T, sink *fakeoutputsink.Sink, keys []string, values []any) {
	t.Helper()

	if !reflect.DeepEqual(sink.Order(), keys) || len(sink.Keys()) != len(keys) {
		t.Fatalf("output keys = %v, want %v", sink.Order(), keys)
	}

	for index, key := range keys {
		body, err := json.Marshal(values[index])
		if err != nil || sink.Single(key) != string(body) {
			t.Fatalf("output %s differs from complete returned plan: %v", key, err)
		}
	}
}

func producedBoundaryConfigPlan(t *testing.T) string {
	t.Helper()
	sink := fakeoutputsink.New(t)

	var diagnostics bytes.Buffer

	err := appconfig.EmitConfigPlan(t.Context(), sink, nil, &diagnostics, output.Annotator{}, appconfig.EmitConfigPlanInput{
		Path: "artifacts.yml",
		FS: fstest.MapFS{"artifacts.yml": {Data: []byte(`
sign:
  method: sigstore
artifacts:
  - name: web
    project-type: npm
    working-directory: apps/web
  - name: lib
    project-type: maven
    working-directory: libs/lib
    build-type: library
    publish-to: [maven-central]
    require-authorization: true
    config:
      settings-path: settings.xml
containers:
  - name: web
    from: [web]
    enable-slsa: false
`)}},
	})
	if err != nil || diagnostics.Len() != 0 {
		t.Fatalf("producer: %v, diagnostics=%q", err, diagnostics.String())
	}

	return sink.Single("config-plan-json")
}
