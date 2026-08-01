// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"testing"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestPublishStagePipeline_FansOutTargets(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleasePublishStagePlan{
		Stage: "publish",
		Targets: pipeline.ReleasePublishTargets{
			MavenCentral: pipeline.TargetPlan[pipeline.PlannedArtifact]{
				Runs: true, Items: []pipeline.PlannedArtifact{{Name: "lib-core", ProjectType: projecttype.Maven}},
			},
			// PlannedContainer target — different item type, same helper.
			Containers: pipeline.TargetPlan[pipeline.PlannedContainer]{
				Runs: true, Items: []pipeline.PlannedContainer{{Name: "app-image"}},
			},
			// npm/google-play/etc not running.
		},
	}

	child := gitlabpipeline.PublishStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
		Version:       "2.3.4",
	})

	if len(child.Include) != 2 {
		t.Fatalf("includes = %d, want 2 (maven-central + container)", len(child.Include))
	}

	mvn := child.Include[0]
	if mvn.Component != "$CI_SERVER_FQDN/diggsweden/reusable-ci/publish-maven-central@1.0.0" {
		t.Errorf("maven-central component = %q", mvn.Component)
	}

	if mvn.Inputs["job-name"] != "publish-maven-central-lib-core" || mvn.Inputs["artifact-name"] != "lib-core" || mvn.Inputs["version"] != "2.3.4" {
		t.Errorf("maven-central inputs = %v", mvn.Inputs)
	}

	// project-type routes multi-ecosystem publish components (publish-forge-packages).
	if mvn.Inputs["project-type"] != "maven" {
		t.Errorf("maven-central project-type = %q, want maven", mvn.Inputs["project-type"])
	}

	cnt := child.Include[1]
	if cnt.Component != "$CI_SERVER_FQDN/diggsweden/reusable-ci/publish-container@1.0.0" {
		t.Errorf("container component = %q", cnt.Component)
	}

	if cnt.Inputs["job-name"] != "publish-container-app-image" {
		t.Errorf("container job-name = %q", cnt.Inputs["job-name"])
	}
}

func TestPublishStagePipeline_EmptyStillValid(t *testing.T) {
	t.Parallel()

	child := gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{Stage: "publish"}, gitlabpipeline.BuildPipelineOptions{ComponentRef: "1.0.0"})
	if len(child.Include) != 0 {
		t.Errorf("includes = %d, want 0", len(child.Include))
	}

	if child.Stages[0] != "publish" {
		t.Errorf("stage = %q, want publish", child.Stages[0])
	}
}
