// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	domainsbom "github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
)

// subject is what an SBOM describes: an artifact's name, its version, and the
// commit it was built from.
//
// The three are meaningless apart — every layer filename is derived from all
// three (domainsbom.BuildLayerFilename, domainsbom.AnalyzedBasename) — so they
// were passed positionally to every function here as `name, version, sha
// string`. Three adjacent same-typed parameters the compiler cannot tell apart:
// a caller that swapped two produced wrongly-named SBOMs and still built.
type subject struct {
	name    string
	version string
	sha     string
}

// layerDeps is the collaborator set a layer needs: the workspace it reads and
// writes through, the tools it drives, and the two writers it reports on.
//
// All of them travel unchanged from generateLayers down to the leaves, so
// passing them positionally added four parameters to every signature in this
// file and forced //nolint:varnamelen onto almost all of them (w, stderr).
//
// syft and gen are both here although no single layer uses both: the build
// layer generates-or-harvests a BOM (gen) while the artifact and container
// layers scan (syft). Splitting them would thread two structs through one call
// tree to avoid one unused field.
type layerDeps struct {
	ws     workspace
	syft   SyftOps
	gen    BuildSBOMGenerator
	w      io.Writer // progress, user-facing
	stderr io.Writer // underlying tool output
}

// generateDualSBOMs emits both an SPDX and a CycloneDX SBOM for the
// given scan target in a single syft invocation. Mirrors
// `generate_dual_sboms` in the bash but batches the two format emits —
// syft's `-o format=file` flag is repeatable, so one scan produces
// both outputs (~halves syft wall-clock per artifact).
func generateDualSBOMs(
	ctx context.Context,
	deps layerDeps,
	scanTarget string,
	subj subject,
	layerName, customBasename string,
) error {
	spdxFile := domainsbom.Filename(customBasename, subj.name, subj.version, layerName, domainsbom.FormatSPDX)
	cdxFile := domainsbom.Filename(customBasename, subj.name, subj.version, layerName, domainsbom.FormatCycloneDX)

	if err := deps.syft.Generate(ctx, scanTarget, map[string]string{
		"spdx-json":      deps.ws.outputPath(spdxFile),
		"cyclonedx-json": deps.ws.outputPath(cdxFile),
	}, deps.stderr); err != nil {
		_, _ = fmt.Fprintf(deps.stderr, "   %s Failed to generate SBOMs for %s\n", clicolor.Cross(deps.stderr), scanTarget)

		return fmt.Errorf("generate sboms for %s: %w", scanTarget, err)
	}

	for _, f := range []string{spdxFile, cdxFile} { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		info, statErr := os.Stat(deps.ws.outputPath(f))
		if statErr != nil || info.Size() == 0 {
			_, _ = fmt.Fprintf(deps.stderr, "   %s Failed to generate: %s\n", clicolor.Cross(deps.stderr), f)

			return fmt.Errorf("generate %s: missing or empty output: %w", f, errs.ErrMissingInput)
		}

		_, _ = fmt.Fprintf(deps.w, "   %s %s (scanned)\n", clicolor.Check(deps.w), f)
	}

	return nil
}

// -----------------------------------------------------------------------
// Build layer
// -----------------------------------------------------------------------

// buildBOMSpec says where a stack's build BOM is found and what to tell the
// user when it is not. Named fields so the per-stack cases below read as data:
// as five positional arguments (two slices then three strings) they wrap across
// lines, and `stack` and `hint` can only be told apart by counting commas
// against the signature.
type buildBOMSpec struct {
	includes []string
	excludes []string
	stack    string
	hint     string
}

func generateBuildLayer(
	ctx context.Context,
	deps layerDeps,
	projectType projecttype.Type,
	subj subject,
) error {
	_, _ = fmt.Fprintln(deps.w, "📦 Assembling Build layer (harvest-or-generate)...")
	_, _ = fmt.Fprintf(deps.w, "   Project type: %s\n", projectType)

	root := domainrelease.DefaultReleaseArtifactsDir
	if _, err := deps.ws.stat(root); err != nil {
		root = "."
	}

	files := maybeGenerateBuildBOM(ctx, deps, projectType, subj.name, root)

	switch projectType {
	case projecttype.Maven:
		emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/target/bom.json"},
			stack:    "Maven",
			hint:     "run cyclonedx-maven-plugin during build",
		})
	case projecttype.NPM:
		emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/bom.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			excludes: []string{"*/node_modules/*"},
			stack:    "npm",
			hint:     "run @cyclonedx/cyclonedx-npm during build",
		})
	case projecttype.Gradle:
		emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/build/reports/bom.json", "*/build/reports/cyclonedx/bom.json"},
			stack:    "Gradle",
			hint:     "run cyclonedx-gradle-plugin during build",
		})
	case projecttype.Cargo:
		emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/bom.json"},
			excludes: []string{"*/target/*"},
			stack:    "Cargo",
			hint:     "run cargo-cyclonedx during build",
		})
	case projecttype.Go:
		emitGoBuildBOM(deps, root, files, subj)
	case projecttype.Python:
		_, _ = fmt.Fprintln(deps.w, "   ⚠️  Build SBOM not implemented for project type: python")
	default:
		_, _ = fmt.Fprintf(deps.w, "   ⚠️  Build SBOM not supported for project type: %s\n", projectType)
	}

	_, _ = fmt.Fprintln(deps.w)

	return nil
}

// maybeGenerateBuildBOM walks the workspace and, when no build byproduct BOM
// exists and a generator is wired, generates one (go/cargo tools read the
// lockfile, so no compiled artifact is needed) — dissolving the old split where
// go used `build go sbom` and cargo shelled out to `cargo cyclonedx`. Returns
// the (re-walked) file list so the harvest step below picks up the new BOM.
// maven/gradle/npm return ErrUnsupported and stay harvest-only.
func maybeGenerateBuildBOM(ctx context.Context, deps layerDeps, projectType projecttype.Type, name, root string) []string {
	files := walkAllFiles(deps.ws, root)

	if deps.gen == nil || !noBuildBOM(files) {
		return files
	}

	if genErr := deps.gen.GenerateBuildSBOM(ctx, projectType, ".", name, deps.stderr); genErr != nil {
		if !errors.Is(genErr, errs.ErrUnsupported) {
			_, _ = fmt.Fprintf(deps.w, "   %s generate build BOM: %v\n", clicolor.Cross(deps.w), genErr)
		}

		return files
	}

	return walkAllFiles(deps.ws, root) // re-walk to pick up the generated bom.json
}

// noBuildBOM reports whether the workspace contains no bom.json anywhere — the
// signal that the build layer must be generated (go/cargo) rather than
// harvested.
func noBuildBOM(files []string) bool {
	return domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
		Files: files, Includes: []string{"*/bom.json", "bom.json"},
	}) == ""
}

// emitGoBuildBOM prefers a BOM under the artifact's own directory and falls
// back to any BOM outside dist/.
func emitGoBuildBOM(deps layerDeps, root string, files []string, subj subject) {
	spec := buildBOMSpec{
		excludes: []string{"*/dist/*"},
		stack:    "Go",
		hint:     "run build-go.yml or sbom-go.yml during release",
	}

	src := ""
	if subj.name != "" {
		src = domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
			Files: files, Includes: []string{fmt.Sprintf("*/%s/bom.json", subj.name)}, Excludes: spec.excludes,
		})
	}

	if src == "" {
		src = domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
			Files: files, Includes: []string{"*/bom.json"}, Excludes: spec.excludes,
		})
	}

	emitBuildBOMSource(deps, root, src, subj, spec)
}

// emitBuildBOM finds the aggregate BOM file (shallowest-match in the
// search root) and copies it to the canonical Build-layer name.
func emitBuildBOM(deps layerDeps, root string, files []string, subj subject, spec buildBOMSpec) {
	src := domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
		Files: files, Includes: spec.includes, Excludes: spec.excludes,
	})
	emitBuildBOMSource(deps, root, src, subj, spec)
}

func emitBuildBOMSource(deps layerDeps, root, src string, subj subject, spec buildBOMSpec) {
	if src == "" {
		if spec.hint != "" {
			_, _ = fmt.Fprintf(deps.w, "   ⚠️  No %s Build SBOM found - %s\n", spec.stack, spec.hint)
		} else {
			_, _ = fmt.Fprintf(deps.w, "   ⚠️  No %s Build SBOM found\n", spec.stack)
		}

		return
	}

	srcPath := src
	if root != "." {
		srcPath = filepath.ToSlash(filepath.Join(root, src))
	}

	out := domainsbom.BuildLayerFilename(subj.name, subj.version, subj.sha)
	if err := copyFile(deps.ws, srcPath, out); err != nil {
		_, _ = fmt.Fprintf(deps.w, "   %s Failed to copy %s → %s: %v\n", clicolor.Cross(deps.w), srcPath, out, err)

		return
	}

	_, _ = fmt.Fprintf(deps.w, "   %s %s (harvested from %s)\n", clicolor.Check(deps.w), out, srcPath)
}

func copyFile(ws workspace, src, dst string) error {
	in, err := ws.open(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(ws.outputPath(dst))
	if err != nil {
		return err
	}

	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return nil
}

// -----------------------------------------------------------------------
// Analyzed-artifact layer
// -----------------------------------------------------------------------

// artifactScan describes one artifact-layer scan: the layer name its SBOMs are
// filed under, the extension to trim from each basename, and what to say when
// no artifact matches.
//
// Gathered because these were three same-typed strings passed positionally and
// interleaved with the subject's — the call was `..., name, version,
// "analyzed-jar", "jar", sha, "No JAR files found", ...`, where `sha` sits
// between two literals and swapping layerType with ext still compiles.
type artifactScan struct {
	layerType   string
	ext         string
	missingWarn string
}

//nolint:cyclop // artifact-layer flow: per-format SBOM generation + filename + summary entry.
func generateArtifactLayer(
	ctx context.Context,
	deps layerDeps,
	projectType projecttype.Type,
	subj subject,
) error {
	_, _ = fmt.Fprintln(deps.w, "📦 Assembling Artifact layer (syft scan)...")
	_, _ = fmt.Fprintf(deps.w, "   Project type: %s\n", projectType)

	jarScan := artifactScan{layerType: "analyzed-jar", ext: "jar", missingWarn: "No JAR files found"}

	switch projectType {
	case projecttype.Maven:
		return scanArtifacts(ctx, deps, findMavenJARs(deps.ws), subj, jarScan)
	case projecttype.NPM:
		return scanArtifacts(ctx, deps, findNPMTarballs(deps.ws), subj, artifactScan{
			layerType: "analyzed-tararchive", ext: "tgz", missingWarn: "No NPM tarball found",
		})
	case projecttype.Gradle:
		if _, err := deps.ws.stat("build/libs"); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				_, _ = fmt.Fprintln(deps.w, "   ⚠️  No build/libs/ directory found")
				_, _ = fmt.Fprintln(deps.w)

				return nil
			}

			return fmt.Errorf("stat build/libs: %w", err)
		}

		return scanArtifacts(ctx, deps, findGradleJARs(deps.ws, subj.name), subj, jarScan)
	case projecttype.Go:
		artifacts := findGoExecutables(deps.ws, subj.name)
		if err := scanArtifacts(ctx, deps, artifacts, subj, artifactScan{
			layerType: "analyzed-binary", missingWarn: "No Go binary found",
		}); err != nil {
			return err
		}

		if len(artifacts) == 0 {
			_, _ = fmt.Fprintln(deps.w, "   Note: Build SBOM from go.mod is usually sufficient")
		}
	case projecttype.Cargo:
		artifacts := findCargoExecutables(deps.ws, subj.name)
		if err := scanArtifacts(ctx, deps, artifacts, subj, artifactScan{
			layerType: "analyzed-binary", missingWarn: "No Rust binary found",
		}); err != nil {
			return err
		}

		if len(artifacts) == 0 {
			_, _ = fmt.Fprintln(deps.w, "   Note: Build SBOM from Cargo.toml is usually sufficient")
		}
	case projecttype.Python:
		return scanPythonWheels(ctx, deps, subj)
	default:
		_, _ = fmt.Fprintf(deps.w, "   ⚠️  Unknown project type: %s\n", projectType)
	}

	_, _ = fmt.Fprintln(deps.w)

	return nil
}

// scanArtifacts emits dual SBOMs for each artifact in artifacts.
// Empty artifact list logs the warning and returns nil (matches the
// bash `|| log_warning ...`).
func scanArtifacts(
	ctx context.Context,
	deps layerDeps,
	artifacts []string,
	subj subject,
	scan artifactScan,
) error {
	if len(artifacts) == 0 {
		_, _ = fmt.Fprintf(deps.w, "   ⚠️  %s\n", scan.missingWarn)

		return nil
	}

	for _, a := range artifacts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		_, _ = fmt.Fprintf(deps.w, "   Scanning: %s\n", a)

		base := filepath.Base(filepath.FromSlash(a))
		if scan.ext != "" {
			base = strings.TrimSuffix(base, "."+scan.ext)
		}

		custom := domainsbom.AnalyzedBasename(base, scan.layerType, subj.sha)
		if err := generateDualSBOMs(ctx, deps, deps.ws.scanTarget(a), subj, scan.layerType, custom); err != nil {
			return err
		}
	}

	return nil
}

// scanPythonWheels handles the .whl / .tar.gz dual-extension trim
// that's specific to Python artifacts.
func scanPythonWheels(ctx context.Context, deps layerDeps, subj subject) error {
	artifacts := findPythonWheels(deps.ws)
	if len(artifacts) == 0 {
		_, _ = fmt.Fprintln(deps.w, "   ⚠️  No Python wheel/sdist found")
		_, _ = fmt.Fprintln(deps.w, "   Note: Source layer SBOM is usually sufficient")

		return nil
	}

	for _, a := range artifacts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		_, _ = fmt.Fprintf(deps.w, "   Scanning: %s\n", a)
		base := filepath.Base(filepath.FromSlash(a))
		base = strings.TrimSuffix(base, ".whl")
		base = strings.TrimSuffix(base, ".tar.gz")

		custom := domainsbom.AnalyzedBasename(base, "analyzed-wheel", subj.sha)
		if err := generateDualSBOMs(ctx, deps, deps.ws.scanTarget(a), subj, "analyzed-wheel", custom); err != nil {
			return err
		}
	}

	return nil
}

// -----------------------------------------------------------------------
// Container layer
// -----------------------------------------------------------------------

func generateContainerLayer(ctx context.Context, deps layerDeps, containerImage string, subj subject) error {
	_, _ = fmt.Fprintln(deps.w, "📦 Assembling Container layer (syft scan)...")

	if containerImage == "" {
		_, _ = fmt.Fprintln(deps.w, "   ⚠️  No container image specified, skipping")
		_, _ = fmt.Fprintln(deps.w)

		return nil
	}

	_, _ = fmt.Fprintf(deps.w, "   Scanning container: %s\n", containerImage)

	custom := domainsbom.AnalyzedBasename(subj.name+"-"+subj.version, "analyzed-container", subj.sha)
	if err := generateDualSBOMs(ctx, deps, containerImage, subj, "analyzed-container", custom); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(deps.w)

	return nil
}

// Artifact-discovery helpers (findMavenJARs etc.) live in discovery.go.
// Filesystem walk primitives (walkMatching etc.) live in walk.go.
