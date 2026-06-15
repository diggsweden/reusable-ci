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

	"github.com/diggsweden/reusable-ci/internal/clicolor"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
	domainsbom "github.com/diggsweden/reusable-ci/internal/domain/sbom"
)

// generateDualSBOMs emits both an SPDX and a CycloneDX SBOM for the
// given scan target in a single syft invocation. Mirrors
// `generate_dual_sboms` in the bash but batches the two format emits —
// syft's `-o format=file` flag is repeatable, so one scan produces
// both outputs (~halves syft wall-clock per artifact).
func generateDualSBOMs(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	scanTarget, name, version, layerName, customBasename string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	spdxFile := domainsbom.Filename(customBasename, name, version, layerName, domainsbom.FormatSPDX)
	cdxFile := domainsbom.Filename(customBasename, name, version, layerName, domainsbom.FormatCycloneDX)

	if err := syft.Generate(ctx, scanTarget, map[string]string{
		"spdx-json":      ws.outputPath(spdxFile),
		"cyclonedx-json": ws.outputPath(cdxFile),
	}, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "   %s Failed to generate SBOMs for %s\n", clicolor.Cross(stderr), scanTarget)

		return fmt.Errorf("generate sboms for %s: %w", scanTarget, err)
	}

	for _, f := range []string{spdxFile, cdxFile} { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		info, statErr := os.Stat(ws.outputPath(f))
		if statErr != nil || info.Size() == 0 {
			_, _ = fmt.Fprintf(stderr, "   %s Failed to generate: %s\n", clicolor.Cross(stderr), f)

			return fmt.Errorf("generate %s: missing or empty output: %w", f, errs.ErrMissingInput)
		}

		_, _ = fmt.Fprintf(w, "   %s %s\n", clicolor.Check(w), f)
	}

	return nil
}

// -----------------------------------------------------------------------
// Build layer
// -----------------------------------------------------------------------

func generateBuildLayer(
	_ context.Context,
	ws workspace,
	projectType projecttype.Type,
	name, version, sha string,
	w io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	_, _ = fmt.Fprintln(w, "📦 Generating Build layer SBOMs...")
	_, _ = fmt.Fprintf(w, "   Project type: %s\n", projectType)

	root := domainrelease.DefaultReleaseArtifactsDir
	if _, err := ws.stat(root); err != nil {
		root = "."
	}

	files := walkAllFiles(ws, root)

	switch projectType {
	case projecttype.Maven:
		emitBuildBOM(ws, root, files, []string{"*/target/bom.json"}, nil,
			name, version, sha, "Maven", "run cyclonedx-maven-plugin during build", w)
	case projecttype.NPM:
		emitBuildBOM(ws, root, files, []string{"*/bom.json"}, []string{"*/node_modules/*"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			name, version, sha, "npm", "run @cyclonedx/cyclonedx-npm during build", w)
	case projecttype.Gradle:
		emitBuildBOM(ws, root, files,
			[]string{"*/build/reports/bom.json", "*/build/reports/cyclonedx/bom.json"},
			nil, name, version, sha, "Gradle",
			"run cyclonedx-gradle-plugin during build", w)
	case projecttype.Cargo:
		emitBuildBOM(ws, root, files, []string{"*/bom.json"}, []string{"*/target/*"},
			name, version, sha, "Cargo", "run cargo-cyclonedx during build", w)
	case projecttype.Go:
		emitGoBuildBOM(ws, root, files, name, version, sha, w)
	case projecttype.Python:
		_, _ = fmt.Fprintln(w, "   ⚠️  Build SBOM not implemented for project type: python")
	default:
		_, _ = fmt.Fprintf(w, "   ⚠️  Build SBOM not supported for project type: %s\n", projectType)
	}

	_, _ = fmt.Fprintln(w)

	return nil
}

func emitGoBuildBOM(ws workspace, root string, files []string, name, version, sha string, w io.Writer) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	excludes := []string{"*/dist/*"}

	src := ""
	if name != "" {
		src = domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
			Files: files, Includes: []string{fmt.Sprintf("*/%s/bom.json", name)}, Excludes: excludes,
		})
	}

	if src == "" {
		src = domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
			Files: files, Includes: []string{"*/bom.json"}, Excludes: excludes,
		})
	}

	emitBuildBOMSource(ws, root, src, name, version, sha, "Go", "run build-go.yml or sbom-go.yml during release", w)
}

// emitBuildBOM finds the aggregate BOM file (shallowest-match in the
// search root) and copies it to the canonical Build-layer name.
func emitBuildBOM(ws workspace, root string, files []string, includes, excludes []string,
	name, version, sha, stack, hint string, w io.Writer,
) {
	src := domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
		Files: files, Includes: includes, Excludes: excludes,
	})
	emitBuildBOMSource(ws, root, src, name, version, sha, stack, hint, w)
}

func emitBuildBOMSource(ws workspace, root, src, name, version, sha, stack, hint string, w io.Writer) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if src == "" {
		if hint != "" {
			_, _ = fmt.Fprintf(w, "   ⚠️  No %s Build SBOM found - %s\n", stack, hint)
		} else {
			_, _ = fmt.Fprintf(w, "   ⚠️  No %s Build SBOM found\n", stack)
		}

		return
	}

	srcPath := src
	if root != "." {
		srcPath = filepath.ToSlash(filepath.Join(root, src))
	}

	out := domainsbom.BuildLayerFilename(name, version, sha)
	if err := copyFile(ws, srcPath, out); err != nil {
		_, _ = fmt.Fprintf(w, "   %s Failed to copy %s → %s: %v\n", clicolor.Cross(w), srcPath, out, err)

		return
	}

	_, _ = fmt.Fprintf(w, "   %s %s\n", clicolor.Check(w), out)
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

//nolint:cyclop // artifact-layer flow: per-format SBOM generation + filename + summary entry.
func generateArtifactLayer(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	projectType projecttype.Type,
	name, version, sha string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	_, _ = fmt.Fprintln(w, "📦 Generating Artifact layer SBOMs...")
	_, _ = fmt.Fprintf(w, "   Project type: %s\n", projectType)

	switch projectType {
	case projecttype.Maven:
		artifacts := findMavenJARs(ws)

		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-jar", "jar", sha, "No JAR files found", w, stderr)
	case projecttype.NPM:
		artifacts := findNPMTarballs(ws)

		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-tararchive", "tgz", sha, "No NPM tarball found", w, stderr)
	case projecttype.Gradle:
		if _, err := ws.stat("build/libs"); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				_, _ = fmt.Fprintln(w, "   ⚠️  No build/libs/ directory found")
				_, _ = fmt.Fprintln(w)

				return nil
			}

			return fmt.Errorf("stat build/libs: %w", err)
		}

		artifacts := findGradleJARs(ws, name)

		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-jar", "jar", sha, "No JAR files found", w, stderr)
	case projecttype.Go:
		artifacts := findGoExecutables(ws, name)
		if err := scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-binary", "", sha, "No Go binary found", w, stderr); err != nil {
			return err
		}

		if len(artifacts) == 0 {
			_, _ = fmt.Fprintln(w, "   Note: Build SBOM from go.mod is usually sufficient")
		}
	case projecttype.Cargo:
		artifacts := findCargoExecutables(ws, name)
		if err := scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-binary", "", sha, "No Rust binary found", w, stderr); err != nil {
			return err
		}

		if len(artifacts) == 0 {
			_, _ = fmt.Fprintln(w, "   Note: Build SBOM from Cargo.toml is usually sufficient")
		}
	case projecttype.Python:
		return scanPythonWheels(ctx, ws, syft, name, version, sha, w, stderr)
	default:
		_, _ = fmt.Fprintf(w, "   ⚠️  Unknown project type: %s\n", projectType)
	}

	_, _ = fmt.Fprintln(w)

	return nil
}

// scanArtifacts emits dual SBOMs for each artifact in artifacts.
// Empty artifact list logs the warning and returns nil (matches the
// bash `|| log_warning ...`).
func scanArtifacts(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	artifacts []string,
	name, version, layerType, ext, sha, missingWarn string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	if len(artifacts) == 0 {
		_, _ = fmt.Fprintf(w, "   ⚠️  %s\n", missingWarn)

		return nil
	}

	for _, a := range artifacts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		_, _ = fmt.Fprintf(w, "   Scanning: %s\n", a)

		base := filepath.Base(filepath.FromSlash(a))
		if ext != "" {
			base = strings.TrimSuffix(base, "."+ext)
		}

		custom := domainsbom.AnalyzedBasename(base, layerType, sha)
		if err := generateDualSBOMs(ctx, ws, syft, ws.scanTarget(a), name, version, layerType, custom, w, stderr); err != nil {
			return err
		}
	}

	return nil
}

// scanPythonWheels handles the .whl / .tar.gz dual-extension trim
// that's specific to Python artifacts.
func scanPythonWheels(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	name, version, sha string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	artifacts := findPythonWheels(ws)
	if len(artifacts) == 0 {
		_, _ = fmt.Fprintln(w, "   ⚠️  No Python wheel/sdist found")
		_, _ = fmt.Fprintln(w, "   Note: Source layer SBOM is usually sufficient")

		return nil
	}

	for _, a := range artifacts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		_, _ = fmt.Fprintf(w, "   Scanning: %s\n", a)
		base := filepath.Base(filepath.FromSlash(a))
		base = strings.TrimSuffix(base, ".whl")
		base = strings.TrimSuffix(base, ".tar.gz")

		custom := domainsbom.AnalyzedBasename(base, "analyzed-wheel", sha)
		if err := generateDualSBOMs(ctx, ws, syft, ws.scanTarget(a), name, version, "analyzed-wheel", custom, w, stderr); err != nil {
			return err
		}
	}

	return nil
}

// -----------------------------------------------------------------------
// Container layer
// -----------------------------------------------------------------------

func generateContainerLayer(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	containerImage, name, version, sha string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	_, _ = fmt.Fprintln(w, "📦 Generating Container layer SBOMs...")

	if containerImage == "" {
		_, _ = fmt.Fprintln(w, "   ⚠️  No container image specified, skipping")
		_, _ = fmt.Fprintln(w)

		return nil
	}

	_, _ = fmt.Fprintf(w, "   Scanning container: %s\n", containerImage)

	custom := domainsbom.AnalyzedBasename(name+"-"+version, "analyzed-container", sha)
	if err := generateDualSBOMs(ctx, ws, syft, containerImage, name, version, "analyzed-container", custom, w, stderr); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w)

	return nil
}

// Artifact-discovery helpers (findMavenJARs etc.) live in discovery.go.
// Filesystem walk primitives (walkMatching etc.) live in walk.go.
