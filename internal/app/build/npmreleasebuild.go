// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/archive"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// NPMReleaseBuildInput drives NPMReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the npm-specific knobs as configuration: the
// SBOM tool version and the build script name.
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

// npmSBOMDeps is the collaborator set the npm SBOM steps share: the summary
// sink they report status to, the npx runner they drive the pinned cyclonedx
// tool through, the annotator they warn on, and the two writers they narrate
// on. They travel unchanged from NPMReleaseBuild into the SBOM leaves, so
// passing them positionally added parameters to each and forced a varnamelen
// waiver (w, stderr).
//
// summary and annot ride along although the innermost leaf that only runs npx
// uses neither; unused fields beat threading a second struct through one call.
// npmRunner is not here: it drives ci/test/pack in the entry only and never
// reaches these leaves.
type npmSBOMDeps struct {
	summary ci.SummarySink
	npx     NPMRunner
	annot   output.Annotator
	w       io.Writer // progress, user-facing
	stderr  io.Writer // underlying tool output
}

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
func NPMReleaseBuild(ctx context.Context, summarySink ci.SummarySink, npmRunner, npxRunner NPMRunner, annot output.Annotator, w, stderr io.Writer, in NPMReleaseBuildInput) error { //nolint:cyclop // metadata, tool steps and evidence each have distinct failure boundaries.
	in.SBOMToolVersion = strings.TrimSpace(in.SBOMToolVersion)
	if in.EnableBuildSBOM && !domainversion.IsStableSemverTag("v"+in.SBOMToolVersion) {
		return fmt.Errorf("SBOM tool version must be an exact stable version: %w", errs.ErrUsage)
	}

	meta, err := resolveNPMMetadata(NPMMetadataInput{Dir: in.Dir, PackageScope: in.PackageScope})
	if err != nil {
		return err
	}

	if npmScopeMismatch(meta.Name, in.PackageScope) {
		annot.Errorf("package.json name must be scoped as %s/<pkg>", in.PackageScope)

		return fmt.Errorf("package name %q does not match scope %q: %w", meta.Name, in.PackageScope, errs.ErrInvalidConfig)
	}

	dir := defaultDir(in.Dir)

	script := in.ScriptName
	if script == "" {
		script = subCmdBuild
	}
	// Check only the selected script now; NPMApplication still re-reads all
	// scripts after npm ci, whose install hooks may repair unselected values.
	if err := preflightNPMScript(dir, script); err != nil {
		return fmt.Errorf("preflight npm build script: %w", err)
	}

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

	sbomDeps := npmSBOMDeps{summary: summarySink, npx: npxRunner, annot: annot, w: w, stderr: stderr}
	if err := npmBuildSBOMStep(ctx, sbomDeps, dir, in.EnableBuildSBOM, in.SBOMToolVersion); err != nil {
		return err
	}

	if err := packNPMRelease(ctx, npmRunner, stderr, dir, meta); err != nil {
		return err
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

func packNPMRelease(ctx context.Context, runner NPMRunner, stderr io.Writer, dir string, meta npmPackageJSON) error { //nolint:cyclop // stage, validate reported and embedded identities, then install; no caller files change before validation.
	stage, err := pathsafe.NewArtifactStaging(dir)
	if err != nil {
		return err
	}
	defer func() { _ = stage.Close() }()

	var output bytes.Buffer
	if err = runner.RunInherit(ctx, dir, &output, stderr, "pack", "--json", "--pack-destination", stage.Root().Name()); err != nil {
		return fmt.Errorf("npm pack: %w", err)
	}

	var packed []struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		Filename string `json:"filename"`
	}
	if json.Unmarshal(output.Bytes(), &packed) != nil || len(packed) != 1 {
		return fmt.Errorf("npm pack must report exactly one package: %w", errs.ErrMalformedInput)
	}

	item := packed[0]
	if item.Name != meta.Name || item.Version != meta.Version || !pathsafe.Relative(item.Filename) || filepath.Base(item.Filename) != item.Filename || !strings.HasSuffix(item.Filename, ".tgz") {
		return fmt.Errorf("npm pack output must match the requested package identity and filename: %w", errs.ErrValidation)
	}

	directory, err := stage.Root().Open(".")
	if err != nil {
		return err
	}

	entries, readErr := directory.ReadDir(-1)
	_ = directory.Close()

	if readErr != nil {
		return readErr
	}

	info, err := stage.Root().Lstat(item.Filename)
	if err != nil || len(entries) != 1 || !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("npm pack did not create one fresh nonempty tarball: %w", errs.ErrValidation)
	}

	if err = stage.Root().Mkdir("inspect", 0o700); err != nil {
		return err
	}

	if err = archive.UntarStripOne(filepath.Join(stage.Root().Name(), item.Filename), filepath.Join(stage.Root().Name(), "inspect")); err != nil {
		return fmt.Errorf("invalid npm tarball: %w: %w", err, errs.ErrMalformedInput)
	}

	metadata, err := cliio.ReadFileInRoot(stage.Root(), "inspect/package.json")
	if err != nil {
		return err
	}

	var packedMeta npmPackageJSON
	if json.Unmarshal(metadata, &packedMeta) != nil || packedMeta != meta {
		return fmt.Errorf("packed package.json does not match the requested identity: %w", errs.ErrValidation)
	}

	if err := stage.Root().RemoveAll("inspect"); err != nil {
		return err
	}

	return stage.Install()
}

// npmBuildSBOMStep generates the Build SBOM via the pinned cyclonedx-npm (run
// with npx) and appends its status block. When enabled, generation is mandatory
// and a failure blocks the build before package publication.
func npmBuildSBOMStep(ctx context.Context, deps npmSBOMDeps, dir string, enabled bool, toolVersion string) error {
	outcome := outcomeSkipped

	var generationErr error

	if enabled {
		outcome = outcomeSuccess

		if err := npmBuildSBOM(ctx, deps, dir, toolVersion); err != nil {
			outcome = outcomeFailure
			generationErr = fmt.Errorf("npm Build SBOM generation failed: %w", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, deps.summary, appsummary.BuildSBOMStatusInput{
		Ecosystem: npmEcosystem,
		Outcome:   outcome,
		WorkDir:   dir,
	}); err != nil {
		return errors.Join(generationErr, fmt.Errorf("write SBOM status: %w", err))
	}

	if generationErr != nil {
		return generationErr
	}

	return nil
}

func npmBuildSBOM(ctx context.Context, deps npmSBOMDeps, dir, toolVersion string) error { //nolint:cyclop // validate the fresh scanner output before replacing the public SBOM.
	version := strings.TrimSpace(toolVersion)
	if version == "" {
		return fmt.Errorf("SBOM tool version is required (set --sbom-tool-version or $CYCLONEDX_VERSION): %w", errs.ErrUsage)
	}

	stage, err := pathsafe.NewArtifactStaging(dir)
	if err != nil {
		return err
	}
	defer func() { _ = stage.Close() }()

	path := filepath.Join(stage.Root().Name(), "bom.json")
	if err = deps.npx.RunInherit(ctx, dir, deps.w, deps.stderr,
		"--yes", "@cyclonedx/cyclonedx-npm@"+version, "--output-format", "json", "--output", path); err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("SBOM generator did not create a fresh regular bom.json: %w", errs.ErrValidation)
	}

	body, err := cliio.ReadFileInRoot(stage.Root(), "bom.json")
	if err != nil {
		return err
	}

	var bom struct {
		Format      string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
	}
	if json.Unmarshal(body, &bom) != nil || bom.Format != "CycloneDX" || bom.SpecVersion == "" || bom.Version < 1 {
		return fmt.Errorf("bom.json must be a CycloneDX document: %w", errs.ErrMalformedInput)
	}

	return stage.Install()
}
