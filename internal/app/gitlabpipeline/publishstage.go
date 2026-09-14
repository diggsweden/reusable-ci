// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// componentPublishContainer is the catalogue component three publish targets
// share: containers, and the container-first Cargo and Go builds, which all
// publish an image rather than a package.
const componentPublishContainer = "publish-container"

// PublishStagePipeline renders only publish targets whose complete execution
// contract can be represented in the generated child pipeline. Artifact-based
// components are refused until the generator can wire their needs/dependencies
// to sibling build jobs; emitting them without that handoff would create jobs
// that cannot access the files they publish.
//
//nolint:cyclop // one "include this component if the target runs" guard per publish target — a flat dispatch table written out.
func PublishStagePipeline(plan pipeline.ReleasePublishStagePlan, opts BuildPipelineOptions) (ChildPipeline, error) {
	if err := validateComponentCoordinate(opts); err != nil {
		return ChildPipeline{}, err
	}

	stage := opts.Stage
	if stage == "" {
		stage = "publish"
	}

	out := ChildPipeline{Stages: []string{stage}}

	tgt := plan.Targets
	for _, contract := range []struct {
		component string
		target    pipeline.TargetPlan[pipeline.PlannedArtifact]
	}{
		{component: "publish-maven-central", target: tgt.MavenCentral},
		{component: "publish-forge-packages", target: tgt.ForgePackages},
		{component: "publish-google-play", target: tgt.GooglePlay},
		{component: "publish-apple-appstore", target: tgt.XcodeIOS},
		{component: componentPublishContainer, target: tgt.CargoContainerFirst},
		{component: componentPublishContainer, target: tgt.GoContainerFirst},
	} {
		if err := validateTargetContract(contract.component, contract.target); err != nil {
			return ChildPipeline{}, err
		}
	}

	if err := validateTargetContract(componentPublishContainer, tgt.Containers); err != nil {
		return ChildPipeline{}, err
	}

	for _, unsupported := range []struct {
		name      string
		component string
		planned   bool
	}{
		{name: "google-play", component: "publish-google-play", planned: targetPlanned(tgt.GooglePlay)},
		{name: "containers", component: componentPublishContainer, planned: targetPlanned(tgt.Containers)},
		{name: "cargo-container-first", component: componentPublishContainer, planned: targetPlanned(tgt.CargoContainerFirst)},
		{name: "go-container-first", component: componentPublishContainer, planned: targetPlanned(tgt.GoContainerFirst)},
	} {
		if unsupported.planned {
			return ChildPipeline{}, unsupportedTarget("publish", unsupported.name, unsupported.component)
		}
	}

	if err := representableProjectTypes(tgt); err != nil {
		return ChildPipeline{}, err
	}

	if err := representableAppStoreItems(tgt.XcodeIOS.Items); err != nil {
		return ChildPipeline{}, err
	}

	for _, artifactTarget := range []struct {
		name      string
		component string
		planned   bool
	}{
		{name: "maven-central", component: "publish-maven-central", planned: tgt.MavenCentral.Runs},
		{name: "forge-packages", component: "publish-forge-packages", planned: tgt.ForgePackages.Runs},
		{name: "apple-appstore", component: "publish-apple-appstore", planned: tgt.XcodeIOS.Runs},
	} {
		if artifactTarget.planned {
			return ChildPipeline{}, unsupportedArtifactHandoff(artifactTarget.name, artifactTarget.component)
		}
	}

	if err := out.validate(); err != nil {
		return ChildPipeline{}, err
	}

	return out, nil
}

// representableProjectTypes rejects artifacts whose project type has no
// equivalent in the GitLab component the target would map to.
func representableProjectTypes(tgt pipeline.ReleasePublishTargets) error {
	for _, artifact := range tgt.ForgePackages.Items {
		if artifact.ProjectType != projecttype.Maven && artifact.ProjectType != projecttype.NPM {
			return fmt.Errorf("GitLab publish target forge-packages cannot represent project type %q: %w", artifact.ProjectType, errs.ErrUnsupported)
		}
	}

	for _, artifact := range tgt.MavenCentral.Items {
		if artifact.ProjectType != projecttype.Maven {
			return fmt.Errorf("GitLab publish target maven-central cannot represent project type %q: %w", artifact.ProjectType, errs.ErrUnsupported)
		}
	}

	return nil
}

// representableAppStoreItems rejects App Store artifacts whose settings the
// generated component cannot express: an unsigned build, a review submission,
// or a macOS version that would have to become a runner image.
func representableAppStoreItems(items []pipeline.PlannedArtifact) error {
	for _, artifact := range items {
		if artifact.ProjectType != projecttype.XcodeIOS {
			return fmt.Errorf("GitLab publish target apple-appstore requires project type xcode-ios, got %q: %w", artifact.ProjectType, errs.ErrInvalidConfig)
		}

		if artifact.XcodeIOS == nil {
			continue
		}

		if artifact.XcodeIOS.EnableCodeSigning != nil && !*artifact.XcodeIOS.EnableCodeSigning {
			return fmt.Errorf("GitLab publish target apple-appstore cannot publish an unsigned xcode-ios artifact: %w", errs.ErrUnsupported)
		}

		if artifact.XcodeIOS.SubmitForReview {
			return fmt.Errorf("GitLab publish target apple-appstore cannot represent submit-for-review: %w", errs.ErrUnsupported)
		}

		if artifact.XcodeIOS.MacOSVersion != "" {
			return fmt.Errorf("GitLab publish target apple-appstore cannot translate macos-version %q to a GitLab runner image: %w", artifact.XcodeIOS.MacOSVersion, errs.ErrUnsupported)
		}
	}

	return nil
}

func unsupportedArtifactHandoff(target, component string) error {
	return fmt.Errorf("GitLab publish target %q requires a build artifact handoff, but generated child pipelines cannot wire needs/dependencies to sibling build jobs for component %q: %w", target, component, errs.ErrUnsupported)
}
