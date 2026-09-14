// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	appplan "github.com/diggsweden/reusable-ci/v3/internal/app/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// decodeOutput decodes one emitted JSON output into want's type.
func decodeOutput[T any](t *testing.T, sink *fakeoutputsink.Sink, key string) T {
	t.Helper()

	var value T
	require.NoError(t, json.Unmarshal([]byte(sink.Single(key)), &value), key)

	return value
}

// runningTargets lists the targets a stage plan output marks as running,
// read from the wire rather than the typed plan.
func runningTargets(t *testing.T, sink *fakeoutputsink.Sink, key string) []string {
	t.Helper()

	var stage struct {
		Targets map[string]struct {
			Runs bool `json:"runs"`
		} `json:"targets"`
	}
	require.NoError(t, json.Unmarshal([]byte(sink.Single(key)), &stage), key)

	running := []string{}

	for name, target := range stage.Targets {
		if target.Runs {
			running = append(running, name)
		}
	}

	sort.Strings(running)

	return running
}

func derivedConfigPlan(t *testing.T, cfg *config.Config) string {
	t.Helper()
	require.NoError(t, config.Derive(cfg))

	return mustConfigPlanJSON(t, pipeline.NewConfigPlan(cfg))
}

// TestPlans_ReturnValueAndEveryProjectionAgree runs the release, snapshot and
// pull-request planners and compares, for each: the exact output key set in
// emission order, the whole plan output with the returned plan, each stage
// and transfer output with the corresponding part of that plan, and the set of
// running targets read independently from the wire. The older tests checked a
// few fields and substrings, so a stage output taken from another stage or a
// transfer plan with a missing item passed.
func TestPlans_ReturnValueAndEveryProjectionAgree(t *testing.T) {
	t.Parallel()

	artifacts := func() *config.Config {
		return &config.Config{
			Sign: config.SignConfig{Method: domainrelease.SignMethodSigstore},
			Artifacts: []config.Artifact{
				{Name: "lib", ProjectType: projecttype.Maven, BuildType: config.BuildTypeLibrary, PublishTo: []config.PublishTarget{config.PublishMavenCentral}},
				{Name: "web", ProjectType: projecttype.NPM},
				{Name: "worker", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
			},
			Containers: []config.Container{{Name: "image", From: []string{"worker"}}},
		}
	}

	t.Run("release", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)
		got, err := appplan.Release(t.Context(), sink, nil, appplan.ReleaseInput{
			ConfigPlanJSON: derivedConfigPlan(t, artifacts()), Branch: "main", RefName: "v1.2.3",
			ReleasePublisher: "github-cli", ReleaseSBOMs: "all", ReleaseSignArtifacts: true, ChangelogCreator: "git-cliff",
		})
		require.NoError(t, err)

		require.Equal(t, []string{"release-plan-json", "prepare-stage-plan-json", "build-stage-plan-json", "publish-stage-plan-json", "artifact-transfer-plan-json"}, sink.Order())
		require.Equal(t, *got, decodeOutput[pipeline.ReleasePlan](t, sink, "release-plan-json"))
		require.Equal(t, got.Stages.Prepare, decodeOutput[pipeline.ReleasePrepareStagePlan](t, sink, "prepare-stage-plan-json"))
		require.Equal(t, got.Stages.Build, decodeOutput[pipeline.ReleaseBuildStagePlan](t, sink, "build-stage-plan-json"))
		require.Equal(t, got.Stages.Publish, decodeOutput[pipeline.ReleasePublishStagePlan](t, sink, "publish-stage-plan-json"))
		require.Equal(t, got.ArtifactTransfers, decodeOutput[pipeline.ArtifactTransferPlan](t, sink, "artifact-transfer-plan-json"))

		require.Equal(t, "main", got.Context.Branch)
		require.Equal(t, "v1.2.3", got.Context.RefName)
		require.Equal(t, []string{"go", "maven", "npm"}, runningTargets(t, sink, "build-stage-plan-json"))
		require.Equal(t, []string{"containers", "maven_central"}, runningTargets(t, sink, "publish-stage-plan-json"))
	})

	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)
		got, err := appplan.SnapshotRelease(t.Context(), sink, appplan.SnapshotReleaseInput{
			ConfigPlanJSON: derivedConfigPlan(t, artifacts()), Branch: "feature/x", ReleaseSHA: "abc123", ReleaseActor: "octocat",
			ReleaseRepository: "org/repo", Registry: "ghcr.io", SBOMs: "all", PublishNPM: true,
		})
		require.NoError(t, err)

		require.Equal(t, []string{"snapshot-release-plan-json", "snapshot-build-stage-plan-json", "snapshot-publish-stage-plan-json", "artifact-transfer-plan-json"}, sink.Order())
		require.Equal(t, *got, decodeOutput[pipeline.SnapshotReleasePlan](t, sink, "snapshot-release-plan-json"))
		require.Equal(t, got.Stages.Build, decodeOutput[pipeline.DevBuildStagePlan](t, sink, "snapshot-build-stage-plan-json"))
		require.Equal(t, got.Stages.Publish, decodeOutput[pipeline.DevPublishStagePlan](t, sink, "snapshot-publish-stage-plan-json"))
		require.Equal(t, got.ArtifactTransfers, decodeOutput[pipeline.ArtifactTransferPlan](t, sink, "artifact-transfer-plan-json"))

		require.Equal(t, "feature/x", got.Context.Branch)
		require.Equal(t, []string{"go", "maven", "npm"}, runningTargets(t, sink, "snapshot-build-stage-plan-json"))
	})

	t.Run("pull request", func(t *testing.T) {
		t.Parallel()

		sink := fakeoutputsink.New(t)
		got, err := appplan.PR(t.Context(), sink, appplan.PRInput{ProjectType: "go", LintEngine: "megalinter", SwiftLint: true})
		require.NoError(t, err)

		require.Equal(t, []string{"pr-plan-json", "quality-stage-plan-json"}, sink.Order())
		require.Equal(t, *got, decodeOutput[pipeline.PRPlan](t, sink, "pr-plan-json"))
		require.Equal(t, got.Stages.Quality, decodeOutput[pipeline.PRQualityStagePlan](t, sink, "quality-stage-plan-json"))
		require.Equal(t, []string{"megalinter", "swift"}, runningTargets(t, sink, "quality-stage-plan-json"))
	})
}
