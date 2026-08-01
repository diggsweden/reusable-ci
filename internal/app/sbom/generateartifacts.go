// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// GenerateArtifactsInput drives `sbom assemble --plan`.
type GenerateArtifactsInput struct {
	ConfigPlanJSON string
	SBOMs          string
	Version        string
	WorkingDir     string
}

type sbomArtifact struct {
	Name              string
	Type              projecttype.Type
	WorkingDir        string
	EffectiveSBOMs    []config.SBOMLayer
	HasEffectiveSBOMs bool
}

// GenerateArtifacts expands release-level SBOM policy and generates artifact-level SBOMs per artifact.
func GenerateArtifacts(
	ctx context.Context,
	syft SyftOps,
	mvn MavenOps,
	gitRepo GitOps,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in GenerateArtifactsInput,
) error {
	layers, err := artifactLayers(in.SBOMs)
	if err != nil {
		return err
	}

	if len(layers) == 0 {
		_, _ = fmt.Fprintln(w, "No artifact-level SBOM layers requested; analyzed-container (if any) is already downloaded.")

		return nil
	}

	artifacts, err := sbomArtifacts(in)
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {
		if artifact.Type == projecttype.Meta {
			continue
		}

		artifactLayers := layers
		if artifact.HasEffectiveSBOMs {
			artifactLayers = intersectSBOMLayers(layers, artifact.EffectiveSBOMs)
		}

		if len(artifactLayers) == 0 {
			continue
		}

		workingDir := artifact.WorkingDir
		if workingDir == "" {
			workingDir = in.WorkingDir
		}

		if err := Generate(ctx, syft, mvn, gitRepo, nil, w, stderr, GenerateInput{
			ProjectType: string(artifact.Type),
			Layers:      sbomLayerCSV(artifactLayers),
			Version:     in.Version,
			Name:        artifact.Name,
			WorkingDir:  workingDir,
		}); err != nil {
			return err
		}
	}

	return nil
}

func artifactLayers(sboms string) ([]config.SBOMLayer, error) {
	layers, err := config.ExpandSBOMs(sboms)
	if err != nil {
		return nil, err
	}

	out := make([]config.SBOMLayer, 0, len(layers))
	for _, layer := range layers {
		if layer == config.SBOMLayerAnalyzedContainer {
			continue
		}

		out = append(out, layer)
	}

	return out, nil
}

func sbomLayerCSV(layers []config.SBOMLayer) string {
	parts := make([]string, 0, len(layers))
	for _, layer := range layers {
		parts = append(parts, string(layer))
	}

	return strings.Join(parts, ",")
}

func intersectSBOMLayers(releaseLayers, artifactLayers []config.SBOMLayer) []config.SBOMLayer {
	allowed := make(map[config.SBOMLayer]bool, len(artifactLayers))
	for _, layer := range artifactLayers {
		allowed[layer] = true
	}

	out := make([]config.SBOMLayer, 0, len(releaseLayers))
	for _, layer := range releaseLayers {
		if allowed[layer] {
			out = append(out, layer)
		}
	}

	return out
}

func sbomArtifacts(in GenerateArtifactsInput) ([]sbomArtifact, error) {
	if strings.TrimSpace(in.ConfigPlanJSON) == "" {
		return nil, fmt.Errorf("config-plan-json is required: %w", errs.ErrUsage)
	}

	return parseConfigPlanSBOMArtifacts(in.ConfigPlanJSON)
}

func parseConfigPlanSBOMArtifacts(value string) ([]sbomArtifact, error) {
	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(value), &plan); err != nil {
		return nil, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ConfigPlanVersion {
		return nil, fmt.Errorf("config-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	out := make([]sbomArtifact, 0, len(plan.Artifacts.All))
	for _, item := range plan.Artifacts.All {
		out = append(out, sbomArtifact{
			Name:              item.Name,
			Type:              item.ProjectType,
			WorkingDir:        item.WorkingDirectory,
			EffectiveSBOMs:    append([]config.SBOMLayer(nil), item.EffectiveSBOMs...),
			HasEffectiveSBOMs: true,
		})
	}

	return out, nil
}
