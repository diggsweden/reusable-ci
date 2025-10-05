// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"errors"
	"strings"
	"testing"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestPublishStagePipeline_RefusesUnrepresentableArtifactHandoff(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		targets pipeline.ReleasePublishTargets
	}{
		{
			name: "maven central",
			targets: pipeline.ReleasePublishTargets{
				MavenCentral: runningTarget(pipeline.PlannedArtifact{Name: "lib", ProjectType: projecttype.Maven}),
			},
		},
		{
			name: "forge packages maven",
			targets: pipeline.ReleasePublishTargets{
				ForgePackages: runningTarget(pipeline.PlannedArtifact{Name: "lib", ProjectType: projecttype.Maven}),
			},
		},
		{
			// npm is a valid component project type. It reaches the shared
			// handoff refusal rather than being rejected as Maven-only.
			name: "forge packages npm",
			targets: pipeline.ReleasePublishTargets{
				ForgePackages: runningTarget(pipeline.PlannedArtifact{Name: "web", ProjectType: projecttype.NPM}),
			},
		},
		{
			name: "apple app store",
			targets: pipeline.ReleasePublishTargets{
				XcodeIOS: runningTarget(pipeline.PlannedArtifact{Name: "ios", ProjectType: projecttype.XcodeIOS}),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{Targets: tc.targets}, gitlabpipeline.BuildPipelineOptions{
				ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
				ComponentRef:  "1.0.0",
			})
			if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), "artifact handoff") {
				t.Fatalf("err = %v, want ErrUnsupported naming the artifact handoff", err)
			}
		})
	}
}

func TestPublishStagePipeline_RejectsZeroJobChildPipeline(t *testing.T) {
	t.Parallel()

	_, err := gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{Stage: "publish"}, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "no generated jobs") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming the zero-job pipeline", err)
	}
}

func TestPublishStagePipeline_RefusesMissingComponent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		plan pipeline.ReleasePublishStagePlan
		want string
	}{
		{
			name: "container",
			plan: pipeline.ReleasePublishStagePlan{Targets: pipeline.ReleasePublishTargets{
				Containers: pipeline.TargetPlan[pipeline.PlannedContainer]{Runs: true, Items: []pipeline.PlannedContainer{{Name: "app"}}},
			}},
			want: "publish-container",
		},
		{
			name: "google play",
			plan: pipeline.ReleasePublishStagePlan{Targets: pipeline.ReleasePublishTargets{
				GooglePlay: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{{Name: "app"}}},
			}},
			want: "publish-google-play",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := gitlabpipeline.PublishStagePipeline(tc.plan, gitlabpipeline.BuildPipelineOptions{
				ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
				ComponentRef:  "1.0.0",
			})
			if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrUnsupported naming absent component %s", err, tc.want)
			}
		})
	}
}

func TestPublishStagePipeline_RequiresExplicitComponentRef(t *testing.T) {
	t.Parallel()

	_, err := gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{}, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "catalog.example/components",
	})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "component ref") {
		t.Fatalf("err = %v, want ErrUsage naming the missing component ref", err)
	}
}

func TestPublishStagePipeline_RejectsUnsupportedForgePackageProjectType(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleasePublishStagePlan{Targets: pipeline.ReleasePublishTargets{
		ForgePackages: pipeline.TargetPlan[pipeline.PlannedArtifact]{
			Runs:  true,
			Items: []pipeline.PlannedArtifact{{Name: "jvm", ProjectType: projecttype.Gradle}},
		},
	}}

	_, err := gitlabpipeline.PublishStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), `cannot represent project type "gradle"`) {
		t.Fatalf("err = %v, want ErrUnsupported naming the unsupported project type", err)
	}
}

func TestPublishStagePipeline_RefusesGitHubSpecificMacOSVersion(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleasePublishStagePlan{Targets: pipeline.ReleasePublishTargets{
		XcodeIOS: pipeline.TargetPlan[pipeline.PlannedArtifact]{
			Runs: true,
			Items: []pipeline.PlannedArtifact{{
				Name: "ios", ProjectType: projecttype.XcodeIOS,
				XcodeIOS: &config.XcodeIOSConfig{MacOSVersion: "macos-26"},
			}},
		},
	}}

	_, err := gitlabpipeline.PublishStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), "macos-version") {
		t.Fatalf("err = %v, want ErrUnsupported naming the untranslatable runner setting", err)
	}
}
