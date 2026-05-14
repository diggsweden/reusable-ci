// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package summary orchestrates `reusable-ci summary ...` subcommands.
// The stage-result writers (build/publish/prepare/pr-quality) each
// compose a typed StageResultEnvelope and dual-write to OutputSink
// (scalars) and ManifestSink (JSON file).
package summary

import (
	"context"
	"fmt"
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

// BuildStageInput drives `summary build-stage-result`. Each *Result
// field is normalised via summary.NormalizeResult; missing/empty
// values fall through to "skipped". *_ARTIFACTS gates whether the
// stage ran at all (any non-"[]" value → ran=true).
type BuildStageInput struct {
	StageName   string // default "build" — dev callers pass "dev-build"
	ProjectType string // optional; included as a manifest extra when set

	// Per-ecosystem results
	MavenResult         string
	NPMResult           string
	GradleResult        string
	GradleAndroidResult string
	XcodeResult         string

	// Per-ecosystem artifact lists (JSON arrays, "[]" when empty)
	MavenArtifacts         string
	NPMArtifacts           string
	GradleArtifacts        string
	GradleAndroidArtifacts string
	XcodeIOSArtifacts      string
}

// BuildStageResult composes the build stage manifest and emits the
// dual-write outputs (stage-ran / stage-result / result-json + manifest).
func BuildStageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in BuildStageInput,
) (*domainsummary.StageResultEnvelope, error) {
	stageName := in.StageName
	if stageName == "" {
		stageName = "build"
	}
	maven := domainsummary.NormalizeResult(in.MavenResult)
	npm := domainsummary.NormalizeResult(in.NPMResult)
	gradle := domainsummary.NormalizeResult(in.GradleResult)
	android := domainsummary.NormalizeResult(in.GradleAndroidResult)
	xcode := domainsummary.NormalizeResult(in.XcodeResult)

	ran := anyNonEmpty(
		in.MavenArtifacts,
		in.NPMArtifacts,
		in.GradleArtifacts,
		in.GradleAndroidArtifacts,
		in.XcodeIOSArtifacts,
	)

	stageResult := domainsummary.StageResult(ran, []domainsummary.Result{maven, npm, gradle, android, xcode})

	env := &domainsummary.StageResultEnvelope{
		Stage:  stageName,
		Result: stageResult,
		Ran:    ran,
		Targets: []domainsummary.Target{
			{Name: "maven", Result: maven},
			{Name: "npm", Result: npm},
			{Name: "gradle", Result: gradle},
			{Name: "gradleandroid", Result: android},
			{Name: "xcodeios", Result: xcode},
		},
	}
	if in.ProjectType != "" {
		env.Extras = []domainsummary.KeyValue{{Key: "project_type", Value: in.ProjectType}}
	}
	if err := emitStageOutputs(ctx, out, manifest, stageName, env); err != nil {
		return nil, err
	}
	return env, nil
}

// PublishStageInput drives `summary publish-stage-result`.
type PublishStageInput struct {
	GHPackagesResult   string // PUBLISH_MAVEN_REGISTRY_RESULT (GitHub Packages registry)
	MavenCentralResult string
	AppStoreResult     string
	GooglePlayResult   string
	ContainersResult   string
	CargoSBOMResult    string

	GHPackagesArtifacts   string
	MavenCentralArtifacts string
	XcodeIOSArtifacts     string
	GooglePlayArtifacts   string
	Containers            string
	CargoArtifacts        string
}

// PublishStageResult composes the publish stage manifest.
func PublishStageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in PublishStageInput,
) (*domainsummary.StageResultEnvelope, error) {
	gh := domainsummary.NormalizeResult(in.GHPackagesResult)
	central := domainsummary.NormalizeResult(in.MavenCentralResult)
	appstore := domainsummary.NormalizeResult(in.AppStoreResult)
	googleplay := domainsummary.NormalizeResult(in.GooglePlayResult)
	containers := domainsummary.NormalizeResult(in.ContainersResult)
	cargo := domainsummary.NormalizeResult(in.CargoSBOMResult)

	ran := anyNonEmpty(
		in.GHPackagesArtifacts,
		in.MavenCentralArtifacts,
		in.XcodeIOSArtifacts,
		in.GooglePlayArtifacts,
		in.Containers,
		in.CargoArtifacts,
	)

	stageResult := domainsummary.StageResult(ran, []domainsummary.Result{
		gh, central, appstore, googleplay, containers, cargo,
	})

	env := &domainsummary.StageResultEnvelope{
		Stage:  "publish",
		Result: stageResult,
		Ran:    ran,
		Targets: []domainsummary.Target{
			{Name: "githubpackages", Result: gh},
			{Name: "mavencentral", Result: central},
			{Name: "appleappstore", Result: appstore},
			{Name: "googleplay", Result: googleplay},
			{Name: "containers", Result: containers},
			{Name: "cargo", Result: cargo},
		},
	}
	if err := emitStageOutputs(ctx, out, manifest, "publish", env); err != nil {
		return nil, err
	}
	return env, nil
}

// PrepareStageInput drives `summary prepare-stage-result`.
type PrepareStageInput struct {
	PrepareReleaseResult string
	ShouldRunVersionBump bool
	Artifacts            string // JSON array; "[]" → no artifacts
}

// PrepareStageResult composes the prepare-stage manifest. Stage runs
// only when both gating inputs are true: the policy says version-bump
// should run AND there are artifacts to bump.
func PrepareStageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in PrepareStageInput,
) (*domainsummary.StageResultEnvelope, error) {
	prep := domainsummary.NormalizeResult(in.PrepareReleaseResult)
	ran := in.ShouldRunVersionBump && in.Artifacts != "" && in.Artifacts != "[]"
	stageResult := domainsummary.ResultSkipped
	if ran {
		stageResult = prep
	}
	env := &domainsummary.StageResultEnvelope{
		Stage:  "prepare",
		Result: stageResult,
		Ran:    ran,
		Targets: []domainsummary.Target{
			{Name: "version-bump", Result: prep},
		},
	}
	if err := emitStageOutputs(ctx, out, manifest, "prepare", env); err != nil {
		return nil, err
	}
	return env, nil
}

// PRQualityStageInput drives `summary pr-quality-stage-result`.
// Each *Enabled flag gates whether the corresponding *Result is included
// in the manifest's targets — disabled targets render as "skipped"
// regardless of the underlying job result.
type PRQualityStageInput struct {
	DependencyReviewResult, DependencyReviewEnabled string
	SASTOpengrepResult, SASTOpengrepEnabled         string
	PublicCodeLintResult, PublicCodeLintEnabled     string
	DevbaseCheckResult, DevbaseCheckEnabled         string
	SwiftResult, SwiftEnabled                       string
}

// PRQualityStageResult composes the pr-quality stage manifest.
// stage-ran is always true (the wrapper job ran by definition); stage
// result aggregates every result regardless of enabled-ness so a job
// failure surfaces even if the lint flag was off when the result was
// collected.
func PRQualityStageResult(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	in PRQualityStageInput,
) (*domainsummary.StageResultEnvelope, error) {
	dep := domainsummary.NormalizeResult(in.DependencyReviewResult)
	sast := domainsummary.NormalizeResult(in.SASTOpengrepResult)
	publiccode := domainsummary.NormalizeResult(in.PublicCodeLintResult)
	devbase := domainsummary.NormalizeResult(in.DevbaseCheckResult)
	swift := domainsummary.NormalizeResult(in.SwiftResult)

	stageResult := domainsummary.AggregateResults([]domainsummary.Result{
		dep, sast, publiccode, devbase, swift,
	})

	effective := func(enabled string, r domainsummary.Result) domainsummary.Result {
		if enabled == "true" {
			return r
		}
		return domainsummary.ResultSkipped
	}
	env := &domainsummary.StageResultEnvelope{
		Stage:  "pr-quality",
		Result: stageResult,
		Ran:    true,
		Targets: []domainsummary.Target{
			{Name: "dependencyreview", Result: effective(in.DependencyReviewEnabled, dep)},
			{Name: "sastopengrep", Result: effective(in.SASTOpengrepEnabled, sast)},
			{Name: "publiccodelint", Result: effective(in.PublicCodeLintEnabled, publiccode)},
			{Name: "devbasecheck", Result: effective(in.DevbaseCheckEnabled, devbase)},
			{Name: "swift", Result: effective(in.SwiftEnabled, swift)},
		},
	}
	if err := emitStageOutputs(ctx, out, manifest, "pr-quality", env); err != nil {
		return nil, err
	}
	return env, nil
}

func anyNonEmpty(jsonArrays ...string) bool {
	for _, v := range jsonArrays {
		if v != "" && v != "[]" {
			return true
		}
	}
	return false
}

func emitStageOutputs(
	ctx context.Context,
	out ci.OutputSink,
	manifest ci.ManifestSink,
	stageName string,
	env *domainsummary.StageResultEnvelope,
) error {
	body, err := env.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal %s envelope: %w", stageName, err)
	}
	if err := manifest.WriteJSON(ctx, stageName, env); err != nil {
		return fmt.Errorf("write %s manifest: %w", stageName, err)
	}
	if err := out.Set(ctx, "stage-ran", strconv.FormatBool(env.Ran)); err != nil {
		return err
	}
	if err := out.Set(ctx, "stage-result", string(env.Result)); err != nil {
		return err
	}
	return out.Set(ctx, "result-json", string(body))
}
