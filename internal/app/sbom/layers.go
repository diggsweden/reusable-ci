// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
		info, statErr := deps.ws.outputInfo(f)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
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

	files, err := maybeGenerateBuildBOM(ctx, deps, projectType, subj.name, root)
	if err != nil {
		return err
	}

	switch projectType {
	case projecttype.Maven:
		return emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/target/bom.json"},
			stack:    "Maven",
			hint:     "run cyclonedx-maven-plugin during build",
		})
	case projecttype.NPM:
		return emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/bom.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			excludes: []string{"*/node_modules/*"},
			stack:    "npm",
			hint:     "run @cyclonedx/cyclonedx-npm during build",
		})
	case projecttype.Gradle, projecttype.GradleAndroid:
		return emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/build/reports/bom.json", "*/build/reports/cyclonedx/bom.json"},
			stack:    string(projectType),
			hint:     "run cyclonedx-gradle-plugin during build",
		})
	case projecttype.Cargo:
		return emitBuildBOM(deps, root, files, subj, buildBOMSpec{
			includes: []string{"*/bom.json"},
			excludes: []string{"*/target/*"},
			stack:    "Cargo",
			hint:     "run cargo-cyclonedx during build",
		})
	case projecttype.Go:
		return emitGoBuildBOM(deps, root, files, subj)
	case projecttype.Python:
		return fmt.Errorf("build SBOM is not implemented for project type python: %w", errs.ErrUnsupported)
	default:
		return fmt.Errorf("build SBOM is not supported for project type %s: %w", projectType, errs.ErrUnsupported)
	}
}

// maybeGenerateBuildBOM walks the workspace and, when no build byproduct BOM
// exists and a generator is wired, generates one (go/cargo tools read the
// lockfile, so no compiled artifact is needed) — dissolving the old split where
// go used `build go sbom` and cargo shelled out to `cargo cyclonedx`. Returns
// the (re-walked) file list so the harvest step below picks up the new BOM.
// maven/gradle/npm return ErrUnsupported and stay harvest-only.
func maybeGenerateBuildBOM(ctx context.Context, deps layerDeps, projectType projecttype.Type, name, root string) ([]string, error) {
	files, err := walkAllFiles(deps.ws, root)
	if err != nil {
		return nil, err
	}

	if deps.gen == nil || !noBuildBOM(files) {
		return files, nil
	}

	if genErr := deps.gen.GenerateBuildSBOM(ctx, projectType, ".", name, deps.stderr); genErr != nil {
		if errors.Is(genErr, errs.ErrUnsupported) {
			return files, nil
		}

		return nil, fmt.Errorf("generate build BOM: %w", genErr)
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
func emitGoBuildBOM(deps layerDeps, root string, files []string, subj subject) error {
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

	return emitBuildBOMSource(deps, root, src, subj, spec)
}

// emitBuildBOM finds the aggregate BOM file (shallowest-match in the
// search root) and copies it to the canonical Build-layer name.
func emitBuildBOM(deps layerDeps, root string, files []string, subj subject, spec buildBOMSpec) error {
	src := domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
		Files: files, Includes: spec.includes, Excludes: spec.excludes,
	})

	return emitBuildBOMSource(deps, root, src, subj, spec)
}

func emitBuildBOMSource(deps layerDeps, root, src string, subj subject, spec buildBOMSpec) error {
	if src == "" {
		if spec.hint != "" {
			_, _ = fmt.Fprintf(deps.w, "   ⚠️  No %s Build SBOM found - %s\n", spec.stack, spec.hint)
		} else {
			_, _ = fmt.Fprintf(deps.w, "   ⚠️  No %s Build SBOM found\n", spec.stack)
		}

		return fmt.Errorf("no %s Build SBOM found: %s: %w", spec.stack, spec.hint, errs.ErrMissingInput)
	}

	srcPath := src
	if root != "." {
		srcPath = filepath.ToSlash(filepath.Join(root, src))
	}

	declared, err := harvestedSubject(deps, srcPath, subj)
	if err != nil {
		return err
	}

	out := domainsbom.BuildLayerFilename(subj.name, subj.version, subj.sha)
	if err := copyFile(deps.ws, srcPath, out); err != nil {
		_, _ = fmt.Fprintf(deps.w, "   %s Failed to copy %s → %s: %v\n", clicolor.Cross(deps.w), srcPath, out, err)

		return fmt.Errorf("copy build SBOM %s to %s: %w", srcPath, out, err)
	}

	_, _ = fmt.Fprintf(deps.w, "   %s %s (harvested from %s, %s)\n", clicolor.Check(deps.w), out, srcPath, describeSubject(declared))

	return nil
}

// harvestedSubject binds the harvested BOM to the release being published.
//
// The file is copied byte-for-byte under a name that asserts a subject, so the
// document has to be the format that name promises and must not declare a
// different release. Discovery walks the workspace for a bom.json, and in a
// multi-module tree the shallowest match can easily belong to another module:
// without this check that module's inventory ships as the release's Build SBOM.
//
// Only the release core is compared, and only when both sides state one. A
// component name is deliberately not required to match: build tools name it by
// module or artifactId while the subject carries a sanitized artifact name, and
// there is no mapping between the two that would not be a guess. An aggregate
// BOM declares no component at all and is accepted as the tree inventory it is.
func harvestedSubject(deps layerDeps, srcPath string, subj subject) (domainsbom.DeclaredSubject, error) {
	body, err := deps.ws.readFile(srcPath)
	if err != nil {
		return domainsbom.DeclaredSubject{}, fmt.Errorf("read harvested build SBOM %s: %w", srcPath, err)
	}

	declared, err := domainsbom.ReadDeclaredSubject(body)
	if err != nil {
		_, _ = fmt.Fprintf(deps.w, "   %s %s: %v\n", clicolor.Cross(deps.w), srcPath, err)

		return domainsbom.DeclaredSubject{}, fmt.Errorf("harvested build SBOM %s: %w", srcPath, err)
	}

	same, stated := domainsbom.SameRelease(declared.Version, subj.version)
	if stated && !same {
		_, _ = fmt.Fprintf(deps.w, "   %s %s declares version %s, not %s\n", clicolor.Cross(deps.w), srcPath, declared.Version, subj.version)

		return domainsbom.DeclaredSubject{}, fmt.Errorf(
			"harvested build SBOM %s describes %q version %s, not the release being published (%s): %w",
			srcPath, declared.Name, declared.Version, subj.version, errs.ErrValidation)
	}

	return declared, nil
}

// describeSubject records what the harvested document claims, so the binding is
// visible in the log rather than only in its refusals.
func describeSubject(declared domainsbom.DeclaredSubject) string {
	if declared.Aggregate {
		return "aggregate BOM, no declared component"
	}

	if declared.Version == "" {
		return fmt.Sprintf("declares %q", declared.Name)
	}

	return fmt.Sprintf("declares %q %s", declared.Name, declared.Version)
}

func copyFile(ws workspace, src, dst string) error {
	in, err := ws.openInputRegular(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := ws.createOutput(dst, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = ws.removeOutput(dst)

		return err
	}

	if err := out.Close(); err != nil {
		_ = ws.removeOutput(dst)

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
		artifacts, err := findMavenJARs(deps.ws)
		if err != nil {
			return err
		}

		return scanArtifacts(ctx, deps, artifacts, subj, jarScan)
	case projecttype.NPM:
		artifacts, err := findNPMTarballs(deps.ws)
		if err != nil {
			return err
		}

		return scanArtifacts(ctx, deps, artifacts, subj, artifactScan{
			layerType: "analyzed-tararchive", ext: "tgz", missingWarn: "No NPM tarball found",
		})
	case projecttype.Gradle:
		if _, err := deps.ws.stat("build/libs"); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				_, _ = fmt.Fprintln(deps.w, "   ⚠️  No build/libs/ directory found")
				_, _ = fmt.Fprintln(deps.w)

				return fmt.Errorf("no Gradle artifacts found in build/libs: %w", errs.ErrMissingInput)
			}

			return fmt.Errorf("stat build/libs: %w", err)
		}

		artifacts, err := findGradleJARs(deps.ws, subj.name)
		if err != nil {
			return err
		}

		return scanArtifacts(ctx, deps, artifacts, subj, jarScan)
	case projecttype.Go:
		artifacts, err := findGoExecutables(deps.ws, subj.name)
		if err != nil {
			return err
		}

		if err := scanArtifacts(ctx, deps, artifacts, subj, artifactScan{
			layerType: "analyzed-binary", missingWarn: "No Go binary found",
		}); err != nil {
			return err
		}

		if len(artifacts) == 0 {
			_, _ = fmt.Fprintln(deps.w, "   Note: Build SBOM from go.mod is usually sufficient")
		}
	case projecttype.Cargo:
		artifacts, err := findCargoExecutables(deps.ws, subj.name)
		if err != nil {
			return err
		}

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
		return fmt.Errorf("analyzed-artifact SBOM is not supported for project type %s: %w", projectType, errs.ErrUnsupported)
	}

	_, _ = fmt.Fprintln(deps.w)

	return nil
}

// scanArtifacts emits dual SBOMs for each artifact in artifacts.
// Empty artifact lists fail closed: reaching this function means the layer was
// explicitly requested, so a warning-only success would publish incomplete
// evidence.
func scanArtifacts(
	ctx context.Context,
	deps layerDeps,
	artifacts []string,
	subj subject,
	scan artifactScan,
) error {
	if len(artifacts) == 0 {
		_, _ = fmt.Fprintf(deps.w, "   ⚠️  %s\n", scan.missingWarn)

		return fmt.Errorf("%s: %w", scan.missingWarn, errs.ErrMissingInput)
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
	artifacts, err := findPythonWheels(deps.ws)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 {
		_, _ = fmt.Fprintln(deps.w, "   ⚠️  No Python wheel/sdist found")
		_, _ = fmt.Fprintln(deps.w, "   Note: Source layer SBOM is usually sufficient")

		return fmt.Errorf("no Python wheel or source distribution found: %w", errs.ErrMissingInput)
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

		return fmt.Errorf("no container image specified for requested analyzed-container SBOM: %w", errs.ErrMissingInput)
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
