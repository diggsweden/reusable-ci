// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// DevPublishStageInput drives `summary dev-publish-stage-result`.
// Mirrors scripts/summary/write-dev-publish-stage-result.sh.
type DevPublishStageInput struct {
	ProjectType projecttype.Type // gates stage-ran (maven/npm/gradle/cargo)
	PublishNPM  bool             // npm target is "skipped" unless this is true and project=npm

	ContainerResult string
	NPMResult       string
	CargoSBOMResult string
	SBOMResult      string

	NPMPackageName    string
	NPMPackageVersion string
	NPMPublishStatus  string // default "published"
}

// DevPublishStageResult composes the dev-publish manifest, dual-writes
// stage-ran/stage-result/result-json + the manifest file, and emits an
// `artifacts-json` output with NPM package metadata.
//
// Stage runs only when project-type is one of {maven, npm, gradle,
// cargo}. The npm target is filtered to "skipped" unless project-type
// is npm AND publish-npm is true — matches the bash gating exactly.
func DevPublishStageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in DevPublishStageInput,
) (*domainsummary.StageResultEnvelope, error) {
	container := domainsummary.NormalizeResult(in.ContainerResult)
	npm := domainsummary.NormalizeResult(in.NPMResult)
	cargoSBOM := domainsummary.NormalizeResult(in.CargoSBOMResult)
	sbom := domainsummary.NormalizeResult(in.SBOMResult)

	ran := false
	switch in.ProjectType {
	case projecttype.Maven, projecttype.NPM, projecttype.Gradle, projecttype.Cargo:
		ran = true
	}

	stageResult := domainsummary.StageResult(ran, []domainsummary.Result{
		container, npm, cargoSBOM, sbom,
	})

	npmTarget := domainsummary.ResultSkipped
	if in.ProjectType == projecttype.NPM && in.PublishNPM {
		npmTarget = npm
	}

	projectType := in.ProjectType
	if projectType == "" {
		projectType = projecttype.Unknown
	}
	env := &domainsummary.StageResultEnvelope{
		Stage:  "dev-publish",
		Result: stageResult,
		Ran:    ran,
		Targets: []domainsummary.Target{
			{Name: "container", Result: container},
			{Name: "npm", Result: npmTarget},
			{Name: "cargo-sbom", Result: cargoSBOM},
			{Name: "sbom", Result: sbom},
		},
		Extras: []domainsummary.KeyValue{
			{Key: "project_type", Value: string(projectType)},
		},
	}

	if err := emitStageOutputs(ctx, out, manifest, "dev-publish", env); err != nil {
		return nil, err
	}

	publishStatus := in.NPMPublishStatus
	if publishStatus == "" {
		publishStatus = "published"
	}
	artifacts := struct {
		NPMPackageName    string `json:"npm_package_name"`
		NPMPackageVersion string `json:"npm_package_version"`
		NPMPublishStatus  string `json:"npm_publish_status"`
	}{
		NPMPackageName:    in.NPMPackageName,
		NPMPackageVersion: in.NPMPackageVersion,
		NPMPublishStatus:  publishStatus,
	}
	body, err := json.Marshal(artifacts)
	if err != nil {
		return nil, fmt.Errorf("marshal artifacts: %w", err)
	}
	if err := out.Set(ctx, "artifacts-json", string(body)); err != nil {
		return nil, fmt.Errorf("set artifacts-json: %w", err)
	}
	return env, nil
}
