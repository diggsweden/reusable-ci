// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package plan holds pure decisions over release/PR/dev-release inputs.
// It produces typed structures the use-case layer renders into platform
// outputs (release-policy-json etc.). No I/O, no env reads, no warnings —
// callers translate domain results into the user-facing emission.
package plan

import (
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// ReleasePlanInputs is the typed envelope of everything the resolver
// reads from. Mirrors the env contract of scripts/plan/resolve-release-plan.sh.
type ReleasePlanInputs struct {
	ReleaseType               string // "stable" | "" | …
	ReleasePublisher          string // "github-cli" | "" | …
	ReleaseCheckAuthorization bool
	ReleaseDraft              bool
	ReleaseSBOMs              string // "all" | "none" | "build,…"
	ReleaseSignArtifacts      bool
	ChangelogCreator          string // "git-cliff" | "" | …
	ChangelogSkipVersionBump  bool
	RefName                   string // CI_REF_NAME (tag name)
	PipelineSBOMs             string // union from parse-artifacts-config
	AnyRequireAuthorization   bool   // any artefact's require_authorization
	HasContainers             bool   // CONTAINERS != []
}

// SBOMConflict describes the case where the release-cap and pipeline
// union intersect to nothing despite both being non-"none". The use case
// renders this as a warning + step-summary block; it never fails the run.
type SBOMConflict struct {
	ReleaseSBOMs  string
	PipelineSBOMs string
}

// ReleasePlan is the resolved policy envelope. Booleans drive workflow
// gating; EffectiveSBOMs is the comma-list intersection (or "none").
type ReleasePlan struct {
	ShouldMakeLatest         bool
	HasContainers            bool
	EffectiveSBOMs           string
	ShouldSignArtifacts      bool
	ShouldCreateRelease      bool
	ShouldCheckAuthorization bool
	ShouldRunVersionBump     bool
	ShouldCreateDraftRelease bool

	// SBOMConflict is non-nil when ReleaseSBOMs and PipelineSBOMs were
	// both non-"none" but their intersection was empty.
	SBOMConflict *SBOMConflict
}

// ResolveReleasePlan computes the eight scalar booleans + effective
// SBOMs from the inputs. Pure: no env, no logging, no warnings.
//
// Returns an error only on malformed SBOM input (ExpandSBOMs failures).
// All other inputs are accepted as-is — empty strings collapse to
// sensible defaults matching the bash.
func ResolveReleasePlan(in ReleasePlanInputs) (*ReleasePlan, error) {
	plan := &ReleasePlan{
		HasContainers:            in.HasContainers,
		ShouldSignArtifacts:      in.ReleaseSignArtifacts,
		ShouldCreateRelease:      in.ReleasePublisher == "github-cli",
		ShouldCheckAuthorization: in.AnyRequireAuthorization || in.ReleaseCheckAuthorization,
		ShouldRunVersionBump:     !in.ChangelogSkipVersionBump && in.ChangelogCreator == "git-cliff",
	}

	plan.ShouldMakeLatest = isStableRelease(in.ReleaseType, in.RefName)
	plan.ShouldCreateDraftRelease = in.ReleaseDraft || isDraftRelease(in.RefName)

	effective, conflict, err := computeEffectiveSBOMs(in.ReleaseSBOMs, in.PipelineSBOMs)
	if err != nil {
		return nil, err
	}
	plan.EffectiveSBOMs = effective
	plan.SBOMConflict = conflict

	return plan, nil
}

// isStableRelease mirrors the bash:
//
//	release_type == "stable"  → stable
//	release_type empty AND tag has no '-' suffix → stable
//	otherwise → not stable
func isStableRelease(releaseType, refName string) bool {
	if releaseType == "stable" {
		return true
	}
	if releaseType == "" && !strings.Contains(refName, "-") {
		return true
	}
	return false
}

// isDraftRelease draft on:
//   - SNAPSHOT tags
//   - non-semver tags (e.g. arbitrary string pushed as a tag)
func isDraftRelease(refName string) bool {
	if validate.IsSnapshot(refName) {
		return true
	}
	if !validate.SemverTagPattern.MatchString(refName) {
		return true
	}
	return false
}

// computeEffectiveSBOMs intersects release-cap with pipeline-union. Both
// are expanded to layer sets; intersection in canonical layer order.
//
// Returns ("none", nil, nil) when the intersection is empty AND either
// side is already "none" (intentional). Returns ("none", &Conflict, nil)
// when both sides are non-"none" but disagree (misconfig).
func computeEffectiveSBOMs(releaseSBOMs, pipelineSBOMs string) (string, *SBOMConflict, error) {
	releaseSet, err := config.ExpandSBOMs(releaseSBOMs)
	if err != nil {
		return "", nil, err
	}
	pipelineSet, err := config.ExpandSBOMs(pipelineSBOMs)
	if err != nil {
		return "", nil, err
	}
	inRelease := layerSet(releaseSet)
	inPipeline := layerSet(pipelineSet)

	canonical := []config.SBOMLayer{
		config.SBOMLayerBuild,
		config.SBOMLayerAnalyzedArtifact,
		config.SBOMLayerAnalyzedContainer,
	}
	parts := make([]string, 0, len(canonical))
	for _, l := range canonical {
		if inRelease[l] && inPipeline[l] {
			parts = append(parts, string(l))
		}
	}
	if len(parts) == 0 {
		var conflict *SBOMConflict
		// Surface a conflict only when both sides were non-"none" — a
		// missing-overlap when either side is "none" is the operator's
		// explicit choice, not a misconfig.
		if releaseSBOMs != "none" && pipelineSBOMs != "none" {
			conflict = &SBOMConflict{
				ReleaseSBOMs:  releaseSBOMs,
				PipelineSBOMs: pipelineSBOMs,
			}
		}
		return "none", conflict, nil
	}
	return strings.Join(parts, ","), nil, nil
}

func layerSet(layers []config.SBOMLayer) map[config.SBOMLayer]bool {
	m := make(map[config.SBOMLayer]bool, len(layers))
	for _, l := range layers {
		m[l] = true
	}
	return m
}
