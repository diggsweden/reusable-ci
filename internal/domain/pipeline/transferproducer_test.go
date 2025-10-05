// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/stretchr/testify/require"
)

func TestConfigPlanExecution_RepeatedFromMatchesProducerContract(t *testing.T) {
	t.Parallel()

	for _, ecosystem := range []struct {
		kind  projecttype.Type
		label string
	}{{projecttype.Go, "Go"}, {projecttype.Cargo, "Cargo"}} {
		for _, testCase := range []struct {
			name           string
			from           []string
			containerFirst bool
			wantRefusal    bool
			wantSlot       string
		}{
			{"single_artifact_first", []string{"cli"}, false, false, "cli"},
			{"repeated_artifact_first", []string{"cli", "cli"}, false, true, "cli"},
			{"repeated_container_first", []string{"cli", "cli"}, true, false, ""},
		} {
			t.Run(string(ecosystem.kind)+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				artifact := config.Artifact{Name: "cli", ProjectType: ecosystem.kind, SBOMs: "none"}
				if testCase.containerFirst {
					if ecosystem.kind == projecttype.Go {
						artifact.Go = &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}
					} else {
						artifact.Cargo = &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}
					}
				}

				cfg := &config.Config{Artifacts: []config.Artifact{artifact}, Containers: []config.Container{{Name: "image", From: testCase.from, EnableSLSA: new(false)}}}
				before := snapshotProducerConfig(t, cfg)

				err := config.Validate(cfg)
				if testCase.wantRefusal {
					require.ErrorIs(t, err, errs.ErrInvalidConfig, "validated producer must reject repeated artifact-first references")
					require.ErrorContains(t, err, "multiple artifact-first "+ecosystem.label+" artifacts [cli cli]")
				} else {
					require.NoError(t, err)
				}

				after := snapshotProducerConfig(t, cfg)
				require.Equal(t, before, after)
				// Derive alone is not the validated producer: it still selects a
				// first slot for rejected input and does not remove a repeated From value.
				require.NoError(t, config.Derive(cfg))
				require.Equal(t, testCase.from, cfg.Containers[0].From)

				slot := cfg.Containers[0].GoArtifactName
				if ecosystem.kind == projecttype.Cargo {
					slot = cfg.Containers[0].CargoArtifactName
				}

				require.Equal(t, testCase.wantSlot, slot)

				plan := pipeline.NewConfigPlan(cfg)
				before, err = json.Marshal(plan)
				require.NoError(t, err)

				err = pipeline.ValidateConfigPlan(plan)
				if testCase.wantRefusal {
					require.ErrorIs(t, err, errs.ErrInvalidConfig)
					require.ErrorContains(t, err, "containers.all[0].from has multiple artifact-first "+ecosystem.label)
				} else {
					require.NoError(t, err)
				}

				after, err = json.Marshal(plan)
				require.NoError(t, err)
				require.Equal(t, before, after)
			})
		}
	}
}

func TestReleaseExecution_RepeatedPlatformsStayProducerLocal(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, platforms string
		suffixes        []string
	}{
		{"exact_repeat", "linux/amd64,linux/amd64", []string{"amd64"}},
		{"nonadjacent_repeats", "linux/arm64,linux/amd64,linux/arm64,linux/arm/v7,linux/amd64", []string{"arm64", "amd64", "arm-v7"}},
		{"same_generated_suffix", "linux/amd64,amd64", []string{"amd64"}},
		{"default", "", []string{"amd64"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Artifacts: []config.Artifact{{Name: "cli", ProjectType: projecttype.Go, SBOMs: "analyzed-container", Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}},
			}
			for _, name := range []string{"image", "other"} {
				cfg.Containers = append(cfg.Containers, config.Container{
					Name: name, From: []string{"cli"}, Platforms: testCase.platforms, EnableSLSA: new(false),
					Extract: &config.ContainerExtract{Binary: &config.ContainerExtractBinary{Target: "export"}},
				})
			}

			require.NoError(t, config.Validate(cfg), "raw producer accepts this platform list")
			require.NoError(t, config.Derive(cfg))
			sourceBefore := snapshotProducerConfig(t, cfg)
			plan := pipeline.NewConfigPlan(cfg)
			planBefore, err := json.Marshal(plan)
			require.NoError(t, err)

			analyzed := make([]pipeline.ArtifactTransfer, 0, 2*len(testCase.suffixes))

			extracted := make([]pipeline.ArtifactTransfer, 0, 2*len(testCase.suffixes))
			for _, name := range []string{"image", "other"} {
				for _, suffix := range testCase.suffixes {
					analyzed = append(analyzed, pipeline.ArtifactTransfer{Kind: "analyzed_container_sbom", NameTemplate: "analyzed-container-sbom-{run_id}-" + name + "-" + suffix, Path: "./sbom-artifacts/", Required: true})
					extracted = append(extracted, pipeline.ArtifactTransfer{Kind: "extracted_binaries", Name: name + "-binaries-" + suffix, Path: "./release-artifacts/binaries/"})
				}
			}

			t.Run("release", func(t *testing.T) {
				release, releaseErr := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "all"})
				require.NoError(t, releaseErr, "same-producer repeated platforms must not become a collision")
				require.Equal(t, pipeline.ArtifactTransferPlan{Version: 1, Items: append(analyzed, extracted...)}, release.ArtifactTransfers)
			})
			t.Run("snapshot", func(t *testing.T) {
				snapshot, snapshotErr := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "analyzed-artifact"})
				require.NoError(t, snapshotErr, "same-producer repeated platforms must not become a collision")
				require.Equal(t, pipeline.ArtifactTransferPlan{Version: 1, Items: extracted}, snapshot.ArtifactTransfers)

				disabled, disabledErr := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "none"})
				require.NoError(t, disabledErr)
				require.Empty(t, disabled.ArtifactTransfers.Items)
			})
			sourceAfter := snapshotProducerConfig(t, cfg)
			require.Equal(t, sourceBefore, sourceAfter)

			planAfter, err := json.Marshal(plan)
			require.NoError(t, err)
			require.Equal(t, planBefore, planAfter)
		})
	}
}

func snapshotProducerConfig(t *testing.T, cfg *config.Config) []byte {
	t.Helper()

	body, err := json.Marshal(cfg) //nolint:musttag // Snapshot all Go fields, not the artifacts.yml wire projection.
	require.NoError(t, err)

	return body
}
