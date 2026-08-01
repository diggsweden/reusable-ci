// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gitlabpipeline emits GitLab CI child-pipeline YAML from the typed
// release plan. GitLab cannot expand a runtime-computed matrix, so plan-driven
// fan-out is materialised as a child pipeline whose YAML is generated from the
// same plan JSON GitHub feeds to `strategy: matrix` — the GitLab provider's
// rendering of the shared contract, not a meta-DSL.
//
// Because each ecosystem's build is now one thin job (`build <eco> run`, exposed
// as the `templates/build-<eco>.yml` components), the generator just emits one
// `include:` per running plan item, pointing at the matching component with
// per-item inputs. No job bodies are synthesised here.
package gitlabpipeline

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"

	"gopkg.in/yaml.v3"
)

// ChildPipeline is the top-level generated child-pipeline document.
type ChildPipeline struct {
	Stages  []string       `yaml:"stages"`
	Include []IncludeEntry `yaml:"include"`
}

// IncludeEntry includes one Catalog component with per-item inputs. Inputs is a
// map so yaml.v3 emits its keys in deterministic (sorted) order.
type IncludeEntry struct {
	Component string            `yaml:"component"`
	Inputs    map[string]string `yaml:"inputs,omitempty"`
}

// BuildPipelineOptions carries what the plan can't supply: where the components
// live and the release version threaded into each build.
type BuildPipelineOptions struct {
	// ComponentBase is the Catalog path prefix, e.g.
	// "$CI_SERVER_FQDN/diggsweden/reusable-ci".
	ComponentBase string
	// ComponentRef pins the component version, e.g. "1.0.0".
	ComponentRef string
	// Version is the release version forwarded to each build component.
	Version string
	// Stage is the child-pipeline stage (default "build").
	Stage string
}

// BuildStagePipeline renders a child pipeline that fans the release build stage
// out over its running targets and items: one `include:` of the matching
// `build-<eco>` component per item. Pure.
func BuildStagePipeline(plan pipeline.ReleaseBuildStagePlan, opts BuildPipelineOptions) ChildPipeline {
	stage := opts.Stage
	if stage == "" {
		stage = "build"
	}

	out := ChildPipeline{Stages: []string{stage}}
	tgt := plan.Targets

	// Each running target contributes one component include per item. Only the
	// ecosystems with a thin `build-<eco>` component are emitted; xcode-ios /
	// gradle-android are specialised (macOS / signing) and degrade away here.
	workdir := func(a pipeline.PlannedArtifact) map[string]string {
		return map[string]string{"working-directory": dirOrDot(a.WorkingDirectory)}
	}

	addTargetIncludes(&out, "build-maven", tgt.Maven, opts, artifactName, func(a pipeline.PlannedArtifact) map[string]string {
		m := workdir(a)
		m["build-type"] = mavenBuildType(a.BuildType)

		return m
	})
	addTargetIncludes(&out, "build-npm", tgt.NPM, opts, artifactName, workdir)
	addTargetIncludes(&out, "build-gradle", tgt.Gradle, opts, artifactName, workdir)
	addTargetIncludes(&out, "build-go", tgt.Go, opts, artifactName, workdir)
	addTargetIncludes(&out, "build-cargo", tgt.Cargo, opts, artifactName, workdir)

	return out
}

// artifactName extracts the plan item's name (works for PlannedArtifact).
func artifactName(a pipeline.PlannedArtifact) string { return a.Name }

// Marshal renders the child pipeline to YAML bytes.
func (c ChildPipeline) Marshal() ([]byte, error) {
	body, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal child pipeline: %w", err)
	}

	return body, nil
}

// addTargetIncludes appends one component include per running item of a target.
// Generic over the item type so build (PlannedArtifact) and publish
// (PlannedArtifact / PlannedContainer) stages share one fan-out helper: the
// job-name is component-scoped + the sanitised item name; artifact-name scopes
// uploads/downloads so multi-item targets don't collide; extra carries the
// target-specific inputs (working-directory, build-type, …).
func addTargetIncludes[T any](out *ChildPipeline, component string, target pipeline.TargetPlan[T], opts BuildPipelineOptions, nameOf func(T) string, extra func(T) map[string]string) {
	if !target.Runs {
		return
	}

	for _, item := range target.Items {
		name := nameOf(item)

		inputs := map[string]string{
			"job-name": component + "-" + sanitizeJobName(name),
			"stage":    out.Stages[0],
		}

		if name != "" {
			inputs["artifact-name"] = name
		}

		if opts.Version != "" {
			inputs["version"] = opts.Version
		}

		if extra != nil {
			for key, value := range extra(item) {
				if value != "" {
					inputs[key] = value
				}
			}
		}

		out.Include = append(out.Include, IncludeEntry{
			Component: fmt.Sprintf("%s/%s@%s", opts.ComponentBase, component, opts.ComponentRef),
			Inputs:    inputs,
		})
	}
}

// mavenBuildType maps the config enum (application/library) to the maven
// component's app/lib input. Empty defaults to app.
func mavenBuildType(buildType config.BuildType) string {
	if buildType == config.BuildTypeLibrary {
		return "lib"
	}

	return "app"
}

func dirOrDot(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "."
	}

	return dir
}

// sanitizeJobName makes an artifact name safe as a GitLab job-name suffix.
func sanitizeJobName(name string) string {
	var buf strings.Builder

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf.WriteRune(r)
		default:
			buf.WriteByte('-')
		}
	}

	out := strings.Trim(buf.String(), "-")
	if out == "" {
		return "artifact"
	}

	return out
}
