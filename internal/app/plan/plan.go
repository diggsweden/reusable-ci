// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package plan orchestrates the `reusable-ci plan ...` subcommands.
// Pure-domain plan composition lives in internal/domain/pipeline; this
// layer drives it, writes outputs, and surfaces SBOM-conflict warnings.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainversion "github.com/diggsweden/reusable-ci/internal/domain/version"
)

// ReleaseInput drives `plan release`.
type ReleaseInput struct {
	ConfigPlanJSON            string
	Branch                    string
	RefName                   string
	FilePattern               string
	ReleaseType               string
	ReleasePublisher          string
	ReleaseRequireAllowlistedSigner bool
	ReleaseDraft              bool
	ReleaseSBOMs              string
	ReleaseSignArtifacts      bool
	ChangelogCreator          string
	ChangelogSkipVersionBump  bool
}

// Release composes the typed release plan outputs consumed by release workflows.
func Release(ctx context.Context, sink ci.OutputSink, summary ci.SummarySink, in ReleaseInput) (*pipeline.ReleasePlan, error) {
	if strings.TrimSpace(in.ConfigPlanJSON) == "" {
		return nil, fmt.Errorf("config-plan-json is required: %w", errs.ErrUsage)
	}

	var configPlan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(in.ConfigPlanJSON), &configPlan); err != nil {
		return nil, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	releasePlan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:                configPlan,
		Branch:                    in.Branch,
		RefName:                   in.RefName,
		FilePattern:               in.FilePattern,
		ReleaseType:               in.ReleaseType,
		ReleasePublisher:          in.ReleasePublisher,
		ReleaseRequireAllowlistedSigner: in.ReleaseRequireAllowlistedSigner,
		ReleaseDraft:              in.ReleaseDraft,
		ReleaseSBOMs:              in.ReleaseSBOMs,
		ReleaseSignArtifacts:      in.ReleaseSignArtifacts,
		ChangelogCreator:          in.ChangelogCreator,
		ChangelogSkipVersionBump:  in.ChangelogSkipVersionBump,
	})
	if err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "release-plan-json", releasePlan); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "prepare-stage-plan-json", releasePlan.Stages.Prepare); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "build-stage-plan-json", releasePlan.Stages.Build); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "publish-stage-plan-json", releasePlan.Stages.Publish); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "artifact-transfer-plan-json", releasePlan.ArtifactTransfers); err != nil {
		return nil, err
	}

	if releasePlan.Policy.SBOMConflict != nil {
		warnSBOMConflict(ctx, summary, releasePlan.Policy.SBOMConflict.ReleaseSBOMs, releasePlan.Policy.SBOMConflict.PipelineSBOMs)
	}

	return &releasePlan, nil
}

func warnSBOMConflict(ctx context.Context, summary ci.SummarySink, releaseSBOMs, pipelineSBOMs string) {
	slog.Warn("sbom misconfiguration: release cap and artefact config have no overlap — no SBOMs will be generated",
		"release_sboms", releaseSBOMs,
		"pipeline_sboms", pipelineSBOMs,
	)

	if summary != nil {
		block := fmt.Sprintf(
			"\n### ⚠️ SBOM misconfiguration\nsboms: release cap %q has no overlap with artefact config %q — no SBOMs will be generated.\nSet the release-level `sboms` input and the per-artefact `sboms` field so they share at least one CISA layer, or use `sboms: none` if intentional.\n",
			releaseSBOMs, pipelineSBOMs,
		)
		_ = summary.Append(ctx, block)
	}
}

// DevReleaseInput drives `plan dev-release`.
type DevReleaseInput struct {
	ConfigPlanJSON      string
	ProjectType         string
	Branch              string
	ReleaseSHA          string
	ReleaseActor        string
	ReleaseRepository   string
	WorkingDirectory    string
	JavaVersion         string
	NodeVersion         string
	RustToolchain       string
	Registry            string
	ReusableCIBinaryRef string
	NPMRegistry         string
	PackageScope        string
	SBOMs               string
	PublishNPM          bool
	UseCIToken          bool
	PublishContainer    bool
}

// DevRelease composes the typed dev-release plan outputs.
func DevRelease(ctx context.Context, sink ci.OutputSink, in DevReleaseInput) (*pipeline.DevReleasePlan, error) {
	if strings.TrimSpace(in.ConfigPlanJSON) == "" {
		return nil, fmt.Errorf("config-plan-json is required: %w", errs.ErrUsage)
	}

	var configPlan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(in.ConfigPlanJSON), &configPlan); err != nil {
		return nil, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	devPlan, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:          configPlan,
		ProjectType:         projecttype.Type(in.ProjectType),
		Branch:              in.Branch,
		ReleaseSHA:          in.ReleaseSHA,
		ReleaseActor:        in.ReleaseActor,
		ReleaseRepository:   in.ReleaseRepository,
		WorkingDirectory:    in.WorkingDirectory,
		JavaVersion:         in.JavaVersion,
		NodeVersion:         in.NodeVersion,
		RustToolchain:       in.RustToolchain,
		Registry:            in.Registry,
		ReusableCIBinaryRef: in.ReusableCIBinaryRef,
		NPMRegistry:         in.NPMRegistry,
		PackageScope:        in.PackageScope,
		SBOMs:               in.SBOMs,
		PublishNPM:          in.PublishNPM,
		UseCIToken:          in.UseCIToken,
		PublishContainer:    in.PublishContainer,
	})
	if err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "dev-release-plan-json", devPlan); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "dev-build-stage-plan-json", devPlan.Stages.Build); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "dev-publish-stage-plan-json", devPlan.Stages.Publish); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "artifact-transfer-plan-json", devPlan.ArtifactTransfers); err != nil {
		return nil, err
	}

	return &devPlan, nil
}

func emitJSONOutput[T any](ctx context.Context, sink ci.OutputSink, key string, value T) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return sink.Set(ctx, key, string(b))
}

// PRInput drives `plan pr`.
type PRInput struct {
	ProjectType                string
	BaseBranch                 string
	ReusableCIBinaryRef        string
	SASTOpengrepRules          string
	SASTOpengrepFailOnSeverity string
	DependencyReview           bool
	SASTOpengrep               bool
	PublicCodeLint             bool
	DevbaseCheck               bool
	SwiftFormat                bool
	SwiftLint                  bool
}

// PR composes typed pull-request quality plan outputs.
func PR(ctx context.Context, sink ci.OutputSink, in PRInput) (*pipeline.PRPlan, error) {
	pt := projecttype.Type(in.ProjectType)
	if !projecttype.IsIn(pt, config.ValidProjectTypes) {
		return nil, fmt.Errorf("unknown project-type %q: %w", in.ProjectType, errs.ErrUsage)
	}

	plan := pipeline.NewPRPlan(pipeline.PRPlanInput{
		ProjectType:                pt,
		BaseBranch:                 in.BaseBranch,
		ReusableCIBinaryRef:        in.ReusableCIBinaryRef,
		SASTOpengrepRules:          in.SASTOpengrepRules,
		SASTOpengrepFailOnSeverity: in.SASTOpengrepFailOnSeverity,
		DependencyReview:           in.DependencyReview,
		SASTOpengrep:               in.SASTOpengrep,
		PublicCodeLint:             in.PublicCodeLint,
		DevbaseCheck:               in.DevbaseCheck,
		SwiftFormat:                in.SwiftFormat,
		SwiftLint:                  in.SwiftLint,
	})
	if err := emitJSONOutput(ctx, sink, "pr-plan-json", plan); err != nil {
		return nil, err
	}

	if err := emitJSONOutput(ctx, sink, "quality-stage-plan-json", plan.Stages.Quality); err != nil {
		return nil, err
	}

	return &plan, nil
}

// GetFilePatternInput drives `plan file-pattern`.
type GetFilePatternInput struct {
	ProjectType   string
	CustomPattern string // when set, used verbatim (caller override)
	WriteToOutput bool   // when true, also emits sink["pattern"]
	Format        output.Format
}

// GetFilePattern returns the pathspec for the project type's version-bump
// commit. Custom override wins. Returns the resolved pattern + emits to
// w (caller's writer) and optionally to the OutputSink.
func GetFilePattern(ctx context.Context, sink ci.OutputSink, out io.Writer, in GetFilePatternInput) (string, error) {
	pattern, err := resolveFilePattern(in)
	if err != nil {
		return "", err
	}

	format := in.Format
	if format == "" || format == output.FormatAuto {
		format = output.FormatText
	}

	if err := emitFilePattern(ctx, sink, out, pattern, format); err != nil {
		return "", err
	}

	if in.WriteToOutput && format != output.FormatGitHub && format != output.FormatGitLab {
		if sink == nil {
			return "", fmt.Errorf("output sink is required: %w", errs.ErrUsage)
		}

		if err := sink.Set(ctx, "pattern", pattern); err != nil {
			return "", err
		}
	}

	return pattern, nil
}

func resolveFilePattern(in GetFilePatternInput) (string, error) {
	if in.ProjectType == "" && in.CustomPattern == "" {
		return "", fmt.Errorf("project type is required: pass --project-type <type> or set $PROJECT_TYPE: %w", errs.ErrUsage)
	}

	if in.CustomPattern != "" {
		return in.CustomPattern, nil
	}

	pt := projecttype.Type(in.ProjectType)
	if !projecttype.IsIn(pt, config.ValidProjectTypes) {
		return "", fmt.Errorf("unknown project-type %q: %w", in.ProjectType, errs.ErrUsage)
	}

	return domainversion.FilePattern(pt), nil
}

func emitFilePattern(ctx context.Context, sink ci.OutputSink, out io.Writer, pattern string, format output.Format) error {
	switch format {
	case output.FormatJSON:
		if out == nil {
			return nil
		}

		body, err := json.Marshal(struct {
			Pattern string `json:"pattern"`
		}{Pattern: pattern})
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintln(out, string(body))
	case output.FormatGitHub, output.FormatGitLab:
		if sink == nil {
			return fmt.Errorf("output sink is required for %s output: %w", format, errs.ErrUsage)
		}

		if err := sink.Set(ctx, "pattern", pattern); err != nil {
			return err
		}
	default:
		if out != nil {
			_, _ = fmt.Fprintln(out, pattern)
		}
	}

	return nil
}
