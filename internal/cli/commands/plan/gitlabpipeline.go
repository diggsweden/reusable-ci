// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
)

// gitlabBuildPipelineCmd materialises a GitLab child pipeline for the release
// build stage. GitLab cannot expand a runtime matrix, so the build fan-out is
// generated from the same plan JSON GitHub feeds to `strategy: matrix`: one
// `include:` of the matching `build-<eco>` component per running plan item.
//
//nolint:dupl // parallel to gitlabPublishPipelineCmd by design — each reads its own stage plan + component set.
func gitlabBuildPipelineCmd() *cli.Command {
	return &cli.Command{
		Name:  "gitlab-build-pipeline",
		Usage: "emit a GitLab child-pipeline YAML that fans the build stage out over the plan",
		Description: `Reads the typed build-stage plan and writes a GitLab child pipeline that
   includes one build-<eco> component per running artifact. Consumed on GitLab
   via trigger:{include:{artifact: <out>}}. The same plan JSON GitHub expands
   natively with strategy: matrix.

EXAMPLE:
   reusable-ci plan gitlab-build-pipeline --component-ref 1.0.0 --output build-pipeline.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "build-stage-plan-json", Sources: cli.EnvVars("BUILD_STAGE_PLAN_JSON"), Usage: "typed build-stage plan JSON (from 'plan release')"},
			&cli.StringFlag{Name: "component-base", Value: "$CI_SERVER_FQDN/diggsweden/reusable-ci", Sources: cli.EnvVars("COMPONENT_BASE"), Usage: "Catalog path prefix for the build-<eco> components"},
			&cli.StringFlag{Name: "component-ref", Required: true, Sources: cli.EnvVars("COMPONENT_REF"), Usage: "component version to pin (e.g. 1.0.0)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "release version forwarded to each build component"},
			&cli.StringFlag{Name: "output", Sources: cli.EnvVars("OUTPUT_FILE"), Usage: "destination path for the child pipeline (default: stdout)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			raw := cmd.String("build-stage-plan-json")
			if raw == "" {
				return fmt.Errorf("--build-stage-plan-json (or $BUILD_STAGE_PLAN_JSON) is required: %w", errs.ErrUsage)
			}

			var plan pipeline.ReleaseBuildStagePlan
			if err := json.Unmarshal([]byte(raw), &plan); err != nil {
				return fmt.Errorf("parse build-stage plan: not valid JSON: %w", errs.ErrInvalidConfig)
			}

			if plan.Version != pipeline.ReleasePlanVersion {
				return fmt.Errorf("unsupported build-stage plan version %d (want %d): %w", plan.Version, pipeline.ReleasePlanVersion, errs.ErrInvalidConfig)
			}

			child := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
				ComponentBase: cmd.String("component-base"),
				ComponentRef:  cmd.String("component-ref"),
				Version:       cmd.String("version"),
				Stage:         plan.Stage,
			})

			return emitChildPipeline(child, cmd.String("output"), "build")
		},
	}
}

// gitlabPublishPipelineCmd is the publish-stage sibling of
// gitlabBuildPipelineCmd: it fans the release publish stage out into one
// `include:` of the matching `publish-<target>` component per running item.
//
//nolint:dupl // parallel to gitlabBuildPipelineCmd by design — each reads its own stage plan + component set.
func gitlabPublishPipelineCmd() *cli.Command {
	return &cli.Command{
		Name:  "gitlab-publish-pipeline",
		Usage: "emit a GitLab child-pipeline YAML that fans the publish stage out over the plan",
		Description: `Reads the typed publish-stage plan and writes a GitLab child pipeline that
   includes one publish-<target> component per running target item (Maven
   Central, GitHub Packages, Google Play, App Store, container). Consumed via
   trigger:{include:{artifact: <out>}}, the publish-side sibling of
   gitlab-build-pipeline.

EXAMPLE:
   reusable-ci plan gitlab-publish-pipeline --component-ref 1.0.0 --output publish-pipeline.yml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "publish-stage-plan-json", Sources: cli.EnvVars("PUBLISH_STAGE_PLAN_JSON"), Usage: "typed publish-stage plan JSON (from 'plan release')"},
			&cli.StringFlag{Name: "component-base", Value: "$CI_SERVER_FQDN/diggsweden/reusable-ci", Sources: cli.EnvVars("COMPONENT_BASE"), Usage: "Catalog path prefix for the publish-<target> components"},
			&cli.StringFlag{Name: "component-ref", Required: true, Sources: cli.EnvVars("COMPONENT_REF"), Usage: "component version to pin (e.g. 1.0.0)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "release version forwarded to each publish component"},
			&cli.StringFlag{Name: "output", Sources: cli.EnvVars("OUTPUT_FILE"), Usage: "destination path for the child pipeline (default: stdout)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			raw := cmd.String("publish-stage-plan-json")
			if raw == "" {
				return fmt.Errorf("--publish-stage-plan-json (or $PUBLISH_STAGE_PLAN_JSON) is required: %w", errs.ErrUsage)
			}

			var plan pipeline.ReleasePublishStagePlan
			if err := json.Unmarshal([]byte(raw), &plan); err != nil {
				return fmt.Errorf("parse publish-stage plan: not valid JSON: %w", errs.ErrInvalidConfig)
			}

			if plan.Version != pipeline.ReleasePlanVersion {
				return fmt.Errorf("unsupported publish-stage plan version %d (want %d): %w", plan.Version, pipeline.ReleasePlanVersion, errs.ErrInvalidConfig)
			}

			child := gitlabpipeline.PublishStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
				ComponentBase: cmd.String("component-base"),
				ComponentRef:  cmd.String("component-ref"),
				Version:       cmd.String("version"),
				Stage:         plan.Stage,
			})

			return emitChildPipeline(child, cmd.String("output"), "publish")
		},
	}
}

// emitChildPipeline marshals a child pipeline and writes it to outPath (or
// stdout when empty). Shared by the build and publish pipeline commands.
func emitChildPipeline(child gitlabpipeline.ChildPipeline, outPath, label string) error {
	body, err := child.Marshal()
	if err != nil {
		return err
	}

	if outPath == "" {
		_, err := os.Stdout.Write(body)

		return err
	}

	if err := os.WriteFile(outPath, body, 0o644); err != nil { //nolint:gosec // child pipeline read by the GitLab runner; 0644 expected.
		return fmt.Errorf("write %q: %w", outPath, err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Wrote GitLab %s child pipeline: %s (%d includes)\n", label, outPath, len(child.Include))

	return nil
}
