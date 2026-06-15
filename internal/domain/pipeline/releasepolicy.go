// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// releasePolicyInputs is the typed envelope of everything the policy
// resolver reads from. Unexported: it is an internal helper input shape
// for resolveReleasePolicy; public callers use ReleasePlanInput.
type releasePolicyInputs struct {
	ReleaseType                     string // "stable" | "" | …
	ReleasePublisher                string // "github-cli" | "" | …
	ReleaseRequireAllowlistedSigner bool
	ReleaseDraft                    bool
	ReleaseSBOMs                    string // "all" | "none" | "build,…"
	ReleaseSignArtifacts            bool
	ChangelogCreator                string // "git-cliff" | "" | …
	ChangelogSkipVersionBump        bool
	RefName                         string // CI_REF_NAME (tag name)
	PipelineSBOMs                   string // union from parse-artifacts-config
	AnyRequireAuthorization         bool   // any artefact's require_authorization
	HasContainers                   bool   // CONTAINERS != []
}

// resolveReleasePolicy computes the typed release policy from workflow
// and config-plan inputs. Pure: no env, no logging, no warnings.
//
// Returns an error only on malformed SBOM input (ExpandSBOMs failures).
// All other inputs are accepted as-is — empty strings collapse to
// sensible defaults matching the bash.
func resolveReleasePolicy(in releasePolicyInputs) (ReleasePolicy, error) {
	policy := ReleasePolicy{
		HasContainers:            in.HasContainers,
		SignArtifacts:            in.ReleaseSignArtifacts,
		CreateRelease:            in.ReleasePublisher == "github-cli",
		RequireAllowlistedSigner: in.AnyRequireAuthorization || in.ReleaseRequireAllowlistedSigner,
		RunVersionBump:           !in.ChangelogSkipVersionBump && in.ChangelogCreator == "git-cliff", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		MakeLatest:               isStableRelease(in.ReleaseType, in.RefName),
		CreateDraftRelease:       in.ReleaseDraft || isDraftRelease(in.RefName),
	}

	effective, conflict, err := computeEffectiveSBOMs(in.ReleaseSBOMs, in.PipelineSBOMs)
	if err != nil {
		return ReleasePolicy{}, err
	}

	policy.SBOMs = effective
	policy.SBOMConflict = conflict

	return policy, nil
}

// isStableRelease classifies a release by its declared type and tag:
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
func computeEffectiveSBOMs(releaseSBOMs, pipelineSBOMs string) (string, *ReleaseSBOMConflict, error) {
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
		var conflict *ReleaseSBOMConflict
		// Surface a conflict only when both sides were non-"none" — a
		// missing-overlap when either side is "none" is the operator's
		// explicit choice, not a misconfig.
		if releaseSBOMs != "none" && pipelineSBOMs != "none" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			conflict = &ReleaseSBOMConflict{
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
