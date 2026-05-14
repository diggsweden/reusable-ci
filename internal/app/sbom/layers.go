// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
	stdout, stderr io.Writer,
) error {
	spdxFile := domainsbom.SBOMFilename(customBasename, name, version, layerName, domainsbom.SBOMFormatSPDX)
	cdxFile := domainsbom.SBOMFilename(customBasename, name, version, layerName, domainsbom.SBOMFormatCycloneDX)

	if err := syft.Generate(ctx, scanTarget, map[string]string{
		"spdx-json":      ws.outputPath(spdxFile),
		"cyclonedx-json": ws.outputPath(cdxFile),
	}, stderr); err != nil {
		fmt.Fprintf(stderr, "   ❌ Failed to generate SBOMs for %s\n", scanTarget)
		return fmt.Errorf("generate sboms for %s: %w", scanTarget, err)
	}
	for _, f := range []string{spdxFile, cdxFile} {
		info, statErr := os.Stat(ws.outputPath(f))
		if statErr != nil || info.Size() == 0 {
			fmt.Fprintf(stderr, "   ❌ Failed to generate: %s\n", f)
			return fmt.Errorf("generate %s: missing or empty output", f)
		}
		fmt.Fprintf(stdout, "   ✅ %s\n", f)
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
	stdout io.Writer,
) error {
	fmt.Fprintln(stdout, "📦 Generating Build layer SBOMs...")
	fmt.Fprintf(stdout, "   Project type: %s\n", projectType)

	root := domainrelease.DefaultReleaseArtifactsDir
	if _, err := ws.stat(root); err != nil {
		root = "."
	}
	files := walkAllFiles(ws, root)

	switch projectType {
	case projecttype.Maven:
		emitBuildBOM(ws, root, files, []string{"*/target/bom.json"}, nil,
			name, version, sha, "Maven", "run cyclonedx-maven-plugin during build", stdout)
	case projecttype.NPM:
		emitBuildBOM(ws, root, files, []string{"*/bom.json"}, []string{"*/node_modules/*"},
			name, version, sha, "npm", "run @cyclonedx/cyclonedx-npm during build", stdout)
	case projecttype.Gradle:
		emitBuildBOM(ws, root, files,
			[]string{"*/build/reports/bom.json", "*/build/reports/cyclonedx/bom.json"},
			nil, name, version, sha, "Gradle",
			"run cyclonedx-gradle-plugin during build", stdout)
	case projecttype.Cargo:
		emitBuildBOM(ws, root, files, []string{"*/bom.json"}, []string{"*/target/*"},
			name, version, sha, "Cargo", "run cargo-cyclonedx during build", stdout)
	case projecttype.Go:
		fmt.Fprintln(stdout, "   ⚠️  Build SBOM not implemented for project type: go")
	case projecttype.Python:
		fmt.Fprintln(stdout, "   ⚠️  Build SBOM not implemented for project type: python")
	default:
		fmt.Fprintf(stdout, "   ⚠️  Build SBOM not supported for project type: %s\n", projectType)
	}
	fmt.Fprintln(stdout)
	return nil
}

// emitBuildBOM finds the aggregate BOM file (shallowest-match in the
// search root) and copies it to the canonical Build-layer name.
func emitBuildBOM(ws workspace, root string, files []string, includes, excludes []string,
	name, version, sha, stack, hint string, stdout io.Writer,
) {
	src := domainsbom.FindBuildBOM(domainsbom.FindBuildBOMInput{
		Files: files, Includes: includes, Excludes: excludes,
	})
	if src == "" {
		if hint != "" {
			fmt.Fprintf(stdout, "   ⚠️  No %s Build SBOM found - %s\n", stack, hint)
		} else {
			fmt.Fprintf(stdout, "   ⚠️  No %s Build SBOM found\n", stack)
		}
		return
	}
	srcPath := src
	if root != "." {
		srcPath = filepath.ToSlash(filepath.Join(root, src))
	}
	out := domainsbom.BuildLayerFilename(name, version, sha)
	if err := copyFile(ws, srcPath, out); err != nil {
		fmt.Fprintf(stdout, "   ❌ Failed to copy %s → %s: %v\n", srcPath, out, err)
		return
	}
	fmt.Fprintf(stdout, "   ✅ %s\n", out)
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

func generateArtifactLayer(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	projectType projecttype.Type,
	name, version, sha string,
	stdout, stderr io.Writer,
) error {
	fmt.Fprintln(stdout, "📦 Generating Artifact layer SBOMs...")
	fmt.Fprintf(stdout, "   Project type: %s\n", projectType)

	switch projectType {
	case projecttype.Maven:
		artifacts := findMavenJARs(ws)
		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-jar", "jar", sha, "No JAR files found", stdout, stderr)
	case projecttype.NPM:
		artifacts := findNPMTarballs(ws)
		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-tararchive", "tgz", sha, "No NPM tarball found", stdout, stderr)
	case projecttype.Gradle:
		if _, err := ws.stat("build/libs"); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintln(stdout, "   ⚠️  No build/libs/ directory found")
				fmt.Fprintln(stdout)
				return nil
			}
			return fmt.Errorf("stat build/libs: %w", err)
		}
		artifacts := findGradleJARs(ws, name)
		return scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-jar", "jar", sha, "No JAR files found", stdout, stderr)
	case projecttype.Go:
		artifacts := findGoExecutables(ws, name)
		if err := scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-binary", "", sha, "No Go binary found", stdout, stderr); err != nil {
			return err
		}
		if len(artifacts) == 0 {
			fmt.Fprintln(stdout, "   Note: Source layer SBOM from go.mod is usually sufficient")
		}
	case projecttype.Cargo:
		artifacts := findCargoExecutables(ws)
		if err := scanArtifacts(ctx, ws, syft, artifacts, name, version, "analyzed-binary", "", sha, "No Rust binary found", stdout, stderr); err != nil {
			return err
		}
		if len(artifacts) == 0 {
			fmt.Fprintln(stdout, "   Note: Source layer SBOM from Cargo.toml is usually sufficient")
		}
	case projecttype.Python:
		return scanPythonWheels(ctx, ws, syft, name, version, sha, stdout, stderr)
	default:
		fmt.Fprintf(stdout, "   ⚠️  Unknown project type: %s\n", projectType)
	}
	fmt.Fprintln(stdout)
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
	stdout, stderr io.Writer,
) error {
	if len(artifacts) == 0 {
		fmt.Fprintf(stdout, "   ⚠️  %s\n", missingWarn)
		return nil
	}
	for _, a := range artifacts {
		fmt.Fprintf(stdout, "   Scanning: %s\n", a)
		base := filepath.Base(filepath.FromSlash(a))
		if ext != "" {
			base = strings.TrimSuffix(base, "."+ext)
		}
		custom := domainsbom.AnalyzedBasename(base, layerType, sha)
		if err := generateDualSBOMs(ctx, ws, syft, ws.scanTarget(a), name, version, layerType, custom, stdout, stderr); err != nil {
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
	stdout, stderr io.Writer,
) error {
	artifacts := findPythonWheels(ws)
	if len(artifacts) == 0 {
		fmt.Fprintln(stdout, "   ⚠️  No Python wheel/sdist found")
		fmt.Fprintln(stdout, "   Note: Source layer SBOM is usually sufficient")
		return nil
	}
	for _, a := range artifacts {
		fmt.Fprintf(stdout, "   Scanning: %s\n", a)
		base := filepath.Base(filepath.FromSlash(a))
		base = strings.TrimSuffix(base, ".whl")
		base = strings.TrimSuffix(base, ".tar.gz")
		custom := domainsbom.AnalyzedBasename(base, "analyzed-wheel", sha)
		if err := generateDualSBOMs(ctx, ws, syft, ws.scanTarget(a), name, version, "analyzed-wheel", custom, stdout, stderr); err != nil {
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
	stdout, stderr io.Writer,
) error {
	fmt.Fprintln(stdout, "📦 Generating Container layer SBOMs...")
	if containerImage == "" {
		fmt.Fprintln(stdout, "   ⚠️  No container image specified, skipping")
		fmt.Fprintln(stdout)
		return nil
	}
	fmt.Fprintf(stdout, "   Scanning container: %s\n", containerImage)
	custom := domainsbom.AnalyzedBasename(name+"-"+version, "analyzed-container", sha)
	if err := generateDualSBOMs(ctx, ws, syft, containerImage, name, version, "analyzed-container", custom, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	return nil
}

// Artifact-discovery helpers (findMavenJARs etc.) live in discovery.go.
// Filesystem walk primitives (walkMatching etc.) live in walk.go.
