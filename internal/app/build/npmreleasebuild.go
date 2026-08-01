// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"
	"strings"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// NPMReleaseBuildInput drives NPMReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the npm-specific knobs — including the variations
// that used to be separate workflow steps (the SBOM tool version, the build
// script name) now folded in as configuration.
type NPMReleaseBuildInput struct {
	ReleaseBuildOptions

	// PackageScope, when set, requires package.json's name to be scoped to it.
	PackageScope string
	// ScriptName is the npm build script; empty defaults to "build".
	ScriptName string
	// SBOMToolVersion pins cyclonedx-npm (run via npx). Renovate-managed in
	// the calling workflow and passed through; required when EnableBuildSBOM.
	SBOMToolVersion string
	// NodeVersion is reported in the build summary only.
	NodeVersion string
}

const npmEcosystem = "npm"

// NPMReleaseBuild runs the whole npm release build as one step: resolve
// metadata, install deps, test (unless skipped), run the build script, generate
// the Build SBOM (unless disabled), pack the tarball, and write the summaries.
//
// It is the npm sibling of GoReleaseBuild — the binary-owned build sequence
// (Design Rule 1). npmRunner drives `npm`; npxRunner drives `npx` for the
// pinned cyclonedx-npm SBOM tool. The tarball is left at its default
// <name>-<version>.tgz path for the forge job to upload by glob, so no scalar
// output (and no $CI_OUTPUT) is needed.
//
//nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
func NPMReleaseBuild(ctx context.Context, summarySink ci.SummarySink, npmRunner, npxRunner NPMRunner, annot output.Annotator, w, stderr io.Writer, in NPMReleaseBuildInput) error {
	meta, err := resolveNPMMetadata(NPMMetadataInput{Dir: in.Dir, PackageScope: in.PackageScope})
	if err != nil {
		return err
	}

	if npmScopeMismatch(meta.Name, in.PackageScope) {
		annot.Errorf("package.json name must be scoped as %s/<pkg>", in.PackageScope)

		return fmt.Errorf("package name %q does not match scope %q: %w", meta.Name, in.PackageScope, errs.ErrInvalidConfig)
	}

	dir := defaultDir(in.Dir)
	_, _ = fmt.Fprintf(w, "NPM release build: %s@%s\n", meta.Name, meta.Version)

	if err := npmRunner.RunInherit(ctx, dir, w, stderr, "ci"); err != nil {
		return fmt.Errorf("npm ci: %w", err)
	}

	if !in.SkipTests {
		// Soft, matching the workflow's `npm test || warn`: a missing/failing
		// test script warns but does not fail the release build (tests are the
		// PR quality stage's gate). Preserved deliberately on consolidation.
		if err := npmRunner.RunInherit(ctx, dir, w, stderr, "test"); err != nil {
			annot.Warningf("npm test failed or no tests configured (continuing): %v", err)
		}
	}

	if err := NPMApplication(ctx, npmRunner, w, stderr, NPMApplicationInput{Dir: dir, ScriptName: in.ScriptName}); err != nil {
		return fmt.Errorf("npm run build: %w", err)
	}

	if err := npmBuildSBOMStep(ctx, summarySink, npxRunner, annot, dir, in.EnableBuildSBOM, in.SBOMToolVersion, w, stderr); err != nil {
		return err
	}

	if err := npmRunner.RunInherit(ctx, dir, w, stderr, "pack"); err != nil {
		return fmt.Errorf("npm pack: %w", err)
	}

	if err := appsummary.NPMBuild(ctx, summarySink, appsummary.NPMBuildInput{
		PackageName: meta.Name,
		Version:     meta.Version,
		NodeVersion: in.NodeVersion,
		SkipTests:   in.SkipTests,
	}); err != nil {
		return fmt.Errorf("write build summary: %w", err)
	}

	return nil
}

// npmBuildSBOMStep generates the Build SBOM via the pinned cyclonedx-npm (run
// with npx) and appends its status block. A failing SBOM warns rather than
// failing the build (it is a best-effort compliance deliverable), mirroring the
// workflow's `if: always()` status step.
func npmBuildSBOMStep(ctx context.Context, summarySink ci.SummarySink, npxRunner NPMRunner, annot output.Annotator, dir string, enabled bool, toolVersion string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names — testing/http/io conventions.
	outcome := outcomeSkipped

	if enabled {
		outcome = outcomeSuccess

		if err := npmBuildSBOM(ctx, npxRunner, dir, toolVersion, w, stderr); err != nil {
			outcome = outcomeFailure

			annot.Warningf("npm Build SBOM generation failed (continuing): %v", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, summarySink, appsummary.BuildSBOMStatusInput{
		Ecosystem: npmEcosystem,
		Outcome:   outcome,
		WorkDir:   dir,
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}

func npmBuildSBOM(ctx context.Context, npxRunner NPMRunner, dir, toolVersion string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names — testing/http/io conventions.
	version := strings.TrimSpace(toolVersion)
	if version == "" {
		return fmt.Errorf("SBOM tool version is required (set --sbom-tool-version or $CYCLONEDX_VERSION): %w", errs.ErrUsage)
	}

	return npxRunner.RunInherit(ctx, dir, w, stderr,
		"--yes", "@cyclonedx/cyclonedx-npm@"+version, "--output-format", "json", "--output", "bom.json")
}
