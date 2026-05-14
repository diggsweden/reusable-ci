// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package plan orchestrates the `reusable-ci plan ...` subcommands.
// Pure-domain decisions live in internal/domain/plan; this layer drives
// them, writes outputs, and surfaces SBOM-conflict warnings.
package plan

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/plan"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainversion "github.com/diggsweden/reusable-ci/internal/domain/version"
)

// ResolveReleasePlan computes and emits the eight scalar outputs +
// effective-sboms. SBOMConflict (release / pipeline disjoint) produces
// a stderr warning and a step-summary block.
func ResolveReleasePlan(
	ctx context.Context,
	sink ci.OutputSink,
	summary ci.SummarySink,
	in plan.ReleasePlanInputs,
) (*plan.ReleasePlan, error) {
	got, err := plan.ResolveReleasePlan(in)
	if err != nil {
		return nil, err
	}

	pairs := []struct {
		key string
		val bool
	}{
		{"should-make-latest", got.ShouldMakeLatest},
		{"has-containers", got.HasContainers},
		{"should-sign-artifacts", got.ShouldSignArtifacts},
		{"should-create-release", got.ShouldCreateRelease},
		{"should-check-authorization", got.ShouldCheckAuthorization},
		{"should-run-version-bump", got.ShouldRunVersionBump},
		{"should-create-draft-release", got.ShouldCreateDraftRelease},
	}
	for _, p := range pairs {
		if err := sink.Set(ctx, p.key, strconv.FormatBool(p.val)); err != nil {
			return nil, err
		}
	}
	if err := sink.Set(ctx, "effective-sboms", got.EffectiveSBOMs); err != nil {
		return nil, err
	}

	if got.SBOMConflict != nil {
		slog.Warn("sbom misconfiguration: release cap and artefact config have no overlap — no SBOMs will be generated",
			"release_sboms", got.SBOMConflict.ReleaseSBOMs,
			"pipeline_sboms", got.SBOMConflict.PipelineSBOMs,
		)
		if summary != nil {
			block := fmt.Sprintf(
				"\n### ⚠️ SBOM misconfiguration\nsboms: release cap %q has no overlap with artefact config %q — no SBOMs will be generated.\nSet the release-level `sboms` input and the per-artefact `sboms` field so they share at least one CISA layer, or use `sboms: none` if intentional.\n",
				got.SBOMConflict.ReleaseSBOMs, got.SBOMConflict.PipelineSBOMs,
			)
			_ = summary.Append(ctx, block)
		}
	}
	return got, nil
}

// WriteReleaseInterface composes ReleasePolicyEnvelope from the
// pre-resolved booleans (env-driven, mirroring the bash) and writes the
// `release-policy-json` output.
type WriteReleaseInterfaceInput struct {
	SignArtifacts      bool
	CheckAuthorization bool
	RunVersionBump     bool
	CreateRelease      bool
	CreateDraftRelease bool
	SBOMs              string
	MakeLatest         bool
	HasContainers      bool
}

// WriteReleaseInterface composes and emits the release-policy-json output.
func WriteReleaseInterface(ctx context.Context, sink ci.OutputSink, in WriteReleaseInterfaceInput) error {
	b, err := plan.MarshalReleasePolicy(plan.ReleasePolicyEnvelope{
		SignArtifacts:      in.SignArtifacts,
		CheckAuthorization: in.CheckAuthorization,
		RunVersionBump:     in.RunVersionBump,
		CreateRelease:      in.CreateRelease,
		CreateDraftRelease: in.CreateDraftRelease,
		SBOMs:              in.SBOMs,
		MakeLatest:         in.MakeLatest,
		HasContainers:      in.HasContainers,
	})
	if err != nil {
		return err
	}
	return sink.Set(ctx, "release-policy-json", string(b))
}

// WriteDevReleaseInterfaceInput drives `plan write-dev-release-interface`.
type WriteDevReleaseInterfaceInput struct {
	plan.DevContext
	plan.DevPolicy
}

// WriteDevReleaseInterface composes and emits dev-context-json + dev-policy-json.
// ProjectType empty errors loudly — matching the bash's "no fallback" gate.
func WriteDevReleaseInterface(ctx context.Context, sink ci.OutputSink, in WriteDevReleaseInterfaceInput) error {
	if in.ProjectType == "" {
		return fmt.Errorf("project-type is empty and no fallback could be derived from artifacts.yml: %w", errs.ErrInvalidConfig)
	}
	if in.RustToolchain == "" {
		in.RustToolchain = "stable"
	}
	cb, err := plan.MarshalDevContext(in.DevContext)
	if err != nil {
		return err
	}
	pb, err := plan.MarshalDevPolicy(in.DevPolicy)
	if err != nil {
		return err
	}
	if err := sink.Set(ctx, "dev-context-json", string(cb)); err != nil {
		return err
	}
	return sink.Set(ctx, "dev-policy-json", string(pb))
}

// WritePRInterfaceInput drives `plan write-pr-interface`.
type WritePRInterfaceInput struct {
	plan.PRContext
	plan.PRPolicy
}

// WritePRInterface composes and emits pr-context-json + pr-policy-json.
func WritePRInterface(ctx context.Context, sink ci.OutputSink, in WritePRInterfaceInput) error {
	policy := plan.BuildPRPolicy(in.PRPolicy)
	cb, err := plan.MarshalPRContext(in.PRContext)
	if err != nil {
		return err
	}
	pb, err := plan.MarshalPRPolicy(policy)
	if err != nil {
		return err
	}
	if err := sink.Set(ctx, "pr-context-json", string(cb)); err != nil {
		return err
	}
	return sink.Set(ctx, "pr-policy-json", string(pb))
}

// GetFilePatternInput drives `plan get-file-pattern`.
type GetFilePatternInput struct {
	ProjectType   string
	CustomPattern string // when set, used verbatim (caller override)
	WriteToOutput bool   // when true, also emits sink["pattern"]
}

// GetFilePattern returns the pathspec for the project type's version-bump
// commit. Custom override wins. Returns the resolved pattern + emits to
// stdout (caller's writer) and optionally to the OutputSink.
func GetFilePattern(ctx context.Context, sink ci.OutputSink, out io.Writer, in GetFilePatternInput) (string, error) {
	if in.ProjectType == "" {
		return "", fmt.Errorf("PROJECT_TYPE is required: %w", errs.ErrUsage)
	}
	pattern := in.CustomPattern
	if pattern == "" {
		pattern = domainversion.FilePattern(projecttype.Type(in.ProjectType))
	}
	fmt.Fprintln(out, pattern)
	if in.WriteToOutput {
		if err := sink.Set(ctx, "pattern", pattern); err != nil {
			return "", err
		}
	}
	return pattern, nil
}
