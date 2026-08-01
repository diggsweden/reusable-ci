// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
)

// PublishStagePipeline renders a child pipeline that fans the release publish
// stage out over its running targets and items — the publish-side sibling of
// BuildStagePipeline. One `include:` of the matching `publish-<target>`
// component per item; no job bodies are synthesised. Pure.
//
// NOTE: components are authored incrementally as `templates/publish-*.yml`.
// Done: publish-maven-central (Sonatype), publish-apple-appstore (App Store
// Connect via altool on a macOS runner), and publish-forge-packages (the
// forge-native registry — GitLab Package Registry via Job-Token). Pending:
// publish-google-play (its GitHub upload is a third-party Action with no
// binary/GitLab equivalent — needs a binary upload command first) and
// publish-container (build-stage-coupled: needs a build-container fan-out and
// per-arch digest aggregation first). The generated includes are structurally
// valid for all; the missing components simply aren't includable until authored.
//
// The per-item input mapping is intentionally minimal until each component pins
// its input contract: job-name, artifact-name, version, and — for the
// artifact-based targets — project-type. project-type is load-bearing for
// publish-forge-packages (dispatches Maven/npm/Gradle on the package ecosystem);
// the others accept it harmlessly. Component names are final.
func PublishStagePipeline(plan pipeline.ReleasePublishStagePlan, opts BuildPipelineOptions) ChildPipeline {
	stage := opts.Stage
	if stage == "" {
		stage = "publish"
	}

	out := ChildPipeline{Stages: []string{stage}}
	tgt := plan.Targets

	// Artifact-based publish targets map one component each; project-type lets a
	// multi-ecosystem component (publish-forge-packages) route on the package kind.
	addTargetIncludes(&out, "publish-maven-central", tgt.MavenCentral, opts, artifactName, projectTypeInput)
	addTargetIncludes(&out, "publish-forge-packages", tgt.ForgePackages, opts, artifactName, projectTypeInput)
	addTargetIncludes(&out, "publish-google-play", tgt.GooglePlay, opts, artifactName, projectTypeInput)
	addTargetIncludes(&out, "publish-apple-appstore", tgt.XcodeIOS, opts, artifactName, projectTypeInput)

	// Container publishes: the index build (PlannedContainer) plus the
	// container-first cargo/go variants (PlannedArtifact) all use publish-container.
	addTargetIncludes(&out, "publish-container", tgt.Containers, opts, containerName, nil)
	addTargetIncludes(&out, "publish-container", tgt.CargoContainerFirst, opts, artifactName, projectTypeInput)
	addTargetIncludes(&out, "publish-container", tgt.GoContainerFirst, opts, artifactName, projectTypeInput)

	return out
}

// containerName extracts a PlannedContainer's name.
func containerName(c pipeline.PlannedContainer) string { return c.Name }

// projectTypeInput forwards the artifact's ecosystem as the project-type input.
// Multi-ecosystem publish components (notably publish-forge-packages, which spans
// Maven/npm/Gradle) need it to route; empty values are dropped by
// addTargetIncludes so single-ecosystem components simply ignore it.
func projectTypeInput(a pipeline.PlannedArtifact) map[string]string {
	return map[string]string{"project-type": string(a.ProjectType)}
}
