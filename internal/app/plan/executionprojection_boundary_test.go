// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	appconfig "github.com/diggsweden/reusable-ci/v3/internal/app/config"
	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/stretchr/testify/require"
)

func TestExecutionProjectionBoundary_LateRefusalsPreserveState(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, reason string
		mutate       func(string) string
	}{
		{"valid", "", func(raw string) string { return raw }},
		{"sboms_all_copies", "artifacts.all[1].effective_sboms", func(raw string) string {
			// The first artifact retains all layers, so the union still agrees.
			return strings.ReplaceAll(raw, `"sboms":"build,analyzed-artifact,analyzed-container"`, `"sboms":"none"`)
		}},
		{"build_name_all_copies", "artifacts.all[1].build_artifact_name", func(raw string) string {
			return strings.ReplaceAll(raw, `"build_artifact_name":"lib-build-artifacts"`, `"build_artifact_name":"foreign"`)
		}},
		{"sbom_name_all_copies", "artifacts.all[1].build_sbom_artifact_name", func(raw string) string {
			return strings.ReplaceAll(raw, `"build_sbom_artifact_name":"lib-build-sbom"`, `"build_sbom_artifact_name":"foreign"`)
		}},
		{"late_from", "containers.all[1].from[2]", func(raw string) string {
			return strings.ReplaceAll(raw, `"from":["api","lib"]`, `"from":["api","lib","missing"]`)
		}},
		{"late_slot", "containers.all[1].maven_artifact_name", func(raw string) string {
			return strings.ReplaceAll(raw, `"maven_artifact_name":"lib-build-artifacts"`, `"maven_artifact_name":"foreign"`)
		}},
		{"canonical_collision", `duplicate concrete name "api-go-build-artifacts"`, func(raw string) string {
			// This also updates every group, From, and the Maven container slot:
			// both producers now canonically transfer api-go-build-artifacts.
			raw = strings.ReplaceAll(raw, `"lib"`, `"api-go"`)

			return strings.ReplaceAll(raw, `"lib-`, `"api-go-`)
		}},
	} {
		for _, seeded := range []bool{false, true} {
			t.Run(testCase.name+map[bool]string{false: "/empty", true: "/seeded"}[seeded], func(t *testing.T) {
				t.Parallel()
				raw := executionBoundaryConfigPlan(t)

				changed := testCase.mutate(raw)
				if testCase.reason != "" {
					require.NotEqual(t, raw, changed, "mutation must reach its target")
				}

				var events []string

				sink := &patternSink{Sink: fakeoutputsink.New(t), events: &events}
				writer := &patternWriter{events: &events}
				summary := &executionSummary{writer: writer, events: &events}

				if seeded {
					require.NoError(t, sink.Sink.Set(t.Context(), "canary", "unchanged"))

					_, err := writer.WriteString("summary canary")
					require.NoError(t, err)
				}

				beforeOutputs, beforeWriter := sink.AllScalar(), writer.String()
				releaseInput := appplan.ReleaseInput{ConfigPlanJSON: changed, ReleaseSBOMs: "all", ReleasePublisher: "github-cli", RefName: "v1.2.3"}
				beforeReleaseInput := releaseInput

				t.Run("release", func(t *testing.T) {
					release, err := appplan.Release(t.Context(), sink, summary, releaseInput)
					if testCase.reason == "" {
						require.NoError(t, err)
						require.NotNil(t, release)
						require.Equal(t, []pipeline.ArtifactTransfer{
							{Kind: "build_artifact", Name: "api-go-build-artifacts", Path: "./release-artifacts/", Required: true},
							{Kind: "build_sbom", Name: "api-go-build-sbom", Path: "./release-artifacts/", Required: true},
							{Kind: "build_artifact", Name: "lib-build-artifacts", Path: "./release-artifacts/", Required: true},
							{Kind: "build_sbom", Name: "lib-build-sbom", Path: "./release-artifacts/", Required: true},
							{Kind: "analyzed_container_sbom", NameTemplate: "analyzed-container-sbom-{run_id}-late-amd64", Path: "./sbom-artifacts/", Required: true},
						}, release.ArtifactTransfers.Items)
						require.Len(t, events, 5)
					} else {
						require.ErrorIs(t, err, errs.ErrInvalidConfig)
						require.ErrorContains(t, err, testCase.reason)
						require.Nil(t, release)
						require.Empty(t, events)
						require.Equal(t, beforeOutputs, sink.AllScalar())
					}

					require.Equal(t, beforeReleaseInput, releaseInput)
					require.Equal(t, beforeWriter, writer.String())
				})

				events = nil
				beforeOutputs = sink.AllScalar()
				snapshotInput := appplan.SnapshotReleaseInput{ConfigPlanJSON: changed, ProjectType: "maven", SBOMs: "all"}
				beforeSnapshotInput := snapshotInput

				t.Run("snapshot", func(t *testing.T) {
					snapshot, err := appplan.SnapshotRelease(t.Context(), sink, snapshotInput)
					if testCase.reason == "" {
						require.NoError(t, err)
						require.NotNil(t, snapshot)
						require.Equal(t, []pipeline.ArtifactTransfer{
							{Kind: "build_artifact", Name: "api-go-build-artifacts", Path: "./release-artifacts/"},
							{Kind: "build_sbom", Name: "api-go-build-sbom", Path: "./release-artifacts/"},
							{Kind: "build_artifact", Name: "lib-build-artifacts", Path: "./release-artifacts/"},
							{Kind: "build_sbom", Name: "lib-build-sbom", Path: "./release-artifacts/"},
						}, snapshot.ArtifactTransfers.Items)
						require.Len(t, events, 4)
					} else {
						require.ErrorIs(t, err, errs.ErrInvalidConfig)
						require.ErrorContains(t, err, testCase.reason)
						require.Nil(t, snapshot)
						require.Empty(t, events)
						require.Equal(t, beforeOutputs, sink.AllScalar())
					}

					require.Equal(t, beforeSnapshotInput, snapshotInput)
					require.Equal(t, beforeWriter, writer.String())
				})
			})
		}
	}
}

func TestExecutionProjectionBoundary_SummaryWriterControl(t *testing.T) {
	t.Parallel()
	raw := executionBoundaryConfigPlan(t)
	raw = strings.ReplaceAll(raw, `"build,analyzed-artifact,analyzed-container"`, `"analyzed-container"`)
	raw = strings.ReplaceAll(raw, `"sboms":"all"`, `"sboms":"analyzed-container"`)
	raw = strings.ReplaceAll(raw, `"effective_sboms":["build","analyzed-artifact","analyzed-container"]`, `"effective_sboms":["analyzed-container"]`)

	var events []string

	sink := &patternSink{Sink: fakeoutputsink.New(t), events: &events}
	writer := &patternWriter{events: &events}
	summary := &executionSummary{writer: writer, events: &events}
	plan, err := appplan.Release(t.Context(), sink, summary, appplan.ReleaseInput{ConfigPlanJSON: raw, ReleaseSBOMs: "build"})
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Equal(t, "none", plan.Policy.SBOMs)
	require.Equal(t, []string{"sink", "sink", "sink", "sink", "sink", "summary", "writer"}, events)
	require.Contains(t, writer.String(), "SBOM misconfiguration")
}

type executionSummary struct {
	writer *patternWriter
	events *[]string
}

func (s *executionSummary) Append(_ context.Context, text string) error {
	*s.events = append(*s.events, "summary")
	_, err := s.writer.Write([]byte(text))

	return err
}

func executionBoundaryConfigPlan(t *testing.T) string {
	t.Helper()

	source := []byte(`artifacts:
  - name: api
    project-type: go
    sboms: all
  - name: lib
    project-type: maven
    sboms: build,analyzed-artifact,analyzed-container
    build-type: library
    publish-to: [forge-packages,maven-central]
containers:
  - name: early
    enable-slsa: false
    enable-scan: false
  - name: late
    from: [api,lib]
    enable-slsa: false
`)
	before := bytes.Clone(source)
	sink := fakeoutputsink.New(t)

	var diagnostics bytes.Buffer
	require.NoError(t, appconfig.EmitConfigPlan(t.Context(), sink, nil, &diagnostics, output.Annotator{}, appconfig.EmitConfigPlanInput{
		Path: "artifacts.yml", FS: fstest.MapFS{"artifacts.yml": {Data: source}},
	}))
	require.Empty(t, diagnostics.String())
	require.Equal(t, before, source)
	// Unknown additive wire fields remain accepted at the real app boundary.
	raw := strings.TrimSuffix(sink.Single("config-plan-json"), "}") + `,"extension":{"future":true}}`

	var plan pipeline.ConfigPlan
	require.NoError(t, json.Unmarshal([]byte(raw), &plan))
	require.Equal(t, 1, plan.Version)
	require.Equal(t, []string{"api", "lib"}, plan.Containers.All[1].From)
	require.Equal(t, "api", plan.Containers.All[1].GoArtifactName)
	require.Equal(t, "lib-build-artifacts", plan.Containers.All[1].MavenArtifactName)

	return raw
}
