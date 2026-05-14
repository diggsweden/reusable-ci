// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package sbom hosts the SBOM-generation use case. Pure logic
// (project-type detection, manifest parsing, filename helpers, build-
// BOM finder) lives in internal/domain/sbom; the orchestrator here
// drives file I/O, syft, maven (for version reads), and git (for the
// short SHA).
package sbom

import (
	"archive/zip"
	"cmp"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainsbom "github.com/diggsweden/reusable-ci/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

// SyftOps abstracts syft for dependency injection. A single Generate
// call scans target once and emits one SBOM file per outputs entry —
// syft natively supports multi-output via repeated `-o format=file`
// flags, so format expansion stays one process per artifact rather
// than one process per (artifact × format) pair.
type SyftOps interface {
	Generate(ctx context.Context, target string, outputs map[string]string, errOut io.Writer) error
}

// MavenOps abstracts the maven adapter for the maven version/name
// helpers.
type MavenOps interface {
	EvalExpression(ctx context.Context, expr string) (string, error)
}

// GitOps is the subset of *git.Repo methods needed by Generate.
type GitOps interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// GenerateInput drives Generate.
type GenerateInput struct {
	ProjectType string // empty / "auto" → detect from manifest files
	Layers      string // CSV; default "build"
	Version     string // override for auto-detected version
	Name        string // override for auto-detected project name
	WorkingDir  string // empty → cwd
	// FS overrides read-only workspace access for tests. It must be rooted at WorkingDir.
	FS             fs.FS
	ContainerImage string // analyzed-container target
	CreateZip      bool
}

// Generate orchestrates the SBOM generation pipeline. Mirrors
// scripts/sbom/generate-sboms.sh end-to-end.
func Generate(
	ctx context.Context,
	syft SyftOps,
	mvn MavenOps,
	gitRepo GitOps,
	stdout, stderr io.Writer,
	in GenerateInput,
) error {
	ws, err := newWorkspace(in)
	if err != nil {
		return err
	}

	layers := in.Layers
	if layers == "" {
		layers = "build"
	}
	parsedLayers := domainsbom.ParseLayerCSV(layers)
	if in.ProjectType != "" && !domainsbom.IsValidProjectType(in.ProjectType) {
		parseErr := (&projecttype.ErrUnknown{Input: in.ProjectType, Valid: domainsbom.ValidProjectTypes}).Error()
		return fmt.Errorf("invalid --project-type %q: %s: %w", in.ProjectType, parseErr, errs.ErrUsage)
	}

	fmt.Fprintln(stdout, "================================================")
	fmt.Fprintln(stdout, "SBOM Generation Script")
	fmt.Fprintln(stdout, "================================================")
	fmt.Fprintf(stdout, "Working directory: %s\n", ws.root)
	fmt.Fprintf(stdout, "Project type: %s\n", cmp.Or(in.ProjectType, string(projecttype.Auto)))
	fmt.Fprintf(stdout, "Requested layers: %s\n\n", layers)

	// Detect project type when "auto" / empty.
	projectType := projecttype.Type(in.ProjectType)
	if projectType == "" || projectType == projecttype.Auto {
		entries, _ := ws.readDir(".")
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		projectType = domainsbom.DetectProjectType(names)
		fmt.Fprintf(stdout, "Auto-detected project type: %s\n", projectType)
	}

	// Resolve name/version.
	resolvedVersion := in.Version
	if resolvedVersion == "" {
		resolvedVersion = readVersion(ctx, ws, mvn, projectType)
	}
	if resolvedVersion == "" {
		resolvedVersion = "unknown"
	}
	resolvedName := in.Name
	if resolvedName == "" {
		resolvedName = readName(ctx, ws, mvn, projectType)
	}
	if resolvedName == "" {
		resolvedName = ws.base()
	}

	fmt.Fprintln(stdout, "Project Information:")
	fmt.Fprintf(stdout, "  Name: %s\n", resolvedName)
	fmt.Fprintf(stdout, "  Version: %s\n", resolvedVersion)
	fmt.Fprintf(stdout, "  Type: %s\n\n", projectType)

	// Short SHA for filename traceability (best-effort).
	sha, _ := gitRepo.Run(ctx, "rev-parse", "--short", "HEAD")
	sha = strings.TrimSpace(sha)

	// Sanitise the components that flow into output filenames.
	safeName := version.SanitizePathToken(resolvedName)
	safeVersion := version.SanitizePathToken(resolvedVersion)

	for _, layer := range parsedLayers {
		if !domainsbom.IsValidLayer(layer) {
			fmt.Fprintf(stdout, "   ⚠️  Unknown layer: %s (valid: build, analyzed-artifact, analyzed-container)\n", layer)
			return domainsbom.UnknownLayerError(layer)
		}
		switch domainsbom.LayerName(layer) {
		case domainsbom.LayerBuild:
			if err := generateBuildLayer(ctx, ws, projectType, safeName, safeVersion, sha, stdout); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedArtifact:
			if err := generateArtifactLayer(ctx, ws, syft, projectType, safeName, safeVersion, sha, stdout, stderr); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedContainer:
			if err := generateContainerLayer(ctx, ws, syft, in.ContainerImage, safeName, safeVersion, sha, stdout, stderr); err != nil {
				return err
			}
		}
	}

	return generateSummary(stdout, ws, resolvedName, resolvedVersion, in.CreateZip)
}

// readVersion fetches the project version per project type. Errors are
// swallowed — the caller falls back to "unknown".
func readVersion(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type) string {
	switch projectType {
	case projecttype.Maven:
		if mvn != nil {
			v, _ := mvn.EvalExpression(ctx, "project.version")
			return strings.TrimSpace(v)
		}
	case projecttype.NPM:
		body, err := ws.readFile("package.json")
		if err == nil {
			return domainsbom.PackageJSONVersion(body)
		}
	case projecttype.Gradle:
		body, err := ws.readFile("build.gradle")
		if err == nil {
			return domainsbom.GradleVersion(body)
		}
	case projecttype.Go:
		body, err := ws.readFile("go.mod")
		if err == nil {
			return domainsbom.GoModuleMajorVersion(body)
		}
	case projecttype.Cargo:
		body, err := ws.readFile("Cargo.toml")
		if err == nil {
			return domainsbom.CargoTOMLVersion(body)
		}
	case projecttype.Python:
		if body, err := ws.readFile("pyproject.toml"); err == nil {
			if v := domainsbom.PyProjectVersion(body); v != "" {
				return v
			}
		}
		if body, err := ws.readFile("setup.py"); err == nil {
			return domainsbom.SetupPyVersion(body)
		}
	}
	return ""
}

// readName fetches the project name per project type.
func readName(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type) string {
	switch projectType {
	case projecttype.Maven:
		if mvn != nil {
			v, _ := mvn.EvalExpression(ctx, "project.artifactId")
			return strings.TrimSpace(v)
		}
	case projecttype.NPM:
		body, err := ws.readFile("package.json")
		if err == nil {
			return domainsbom.PackageJSONName(body)
		}
	case projecttype.Gradle:
		body, err := ws.readFile("settings.gradle")
		if err == nil {
			return domainsbom.GradleRootProjectName(body)
		}
	case projecttype.Go:
		body, err := ws.readFile("go.mod")
		if err == nil {
			return domainsbom.GoModuleName(body)
		}
	case projecttype.Cargo:
		body, err := ws.readFile("Cargo.toml")
		if err == nil {
			return domainsbom.CargoTOMLName(body)
		}
	case projecttype.Python:
		if body, err := ws.readFile("pyproject.toml"); err == nil {
			if n := domainsbom.PyProjectName(body); n != "" {
				return n
			}
		}
		if body, err := ws.readFile("setup.py"); err == nil {
			return domainsbom.SetupPyName(body)
		}
	}
	return ""
}

// generateSummary mirrors the `generate_summary` function: lists the
// SBOMs produced in the working dir, optionally zips them.
func generateSummary(stdout io.Writer, ws workspace, projectName, ver string, createZip bool) error {
	fmt.Fprintln(stdout, "================================================")
	fmt.Fprintln(stdout, "SBOM Generation Complete")
	fmt.Fprintln(stdout, "================================================")

	matches := listSBOMs(ws, ".")
	if len(matches) == 0 {
		fmt.Fprintln(stdout, "❌ No SBOM files generated")
		return fmt.Errorf("no SBOM files generated: %w", errs.ErrValidation)
	}
	fmt.Fprintf(stdout, "✅ Successfully generated %d SBOM files\n\n", len(matches))
	fmt.Fprintln(stdout, "Generated files:")
	for _, m := range matches {
		info, _ := os.Stat(ws.outputPath(m))
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Fprintf(stdout, "  %8d  %s\n", size, m)
	}
	fmt.Fprintln(stdout)

	if createZip {
		zipName := domainsbom.ZipName(version.SanitizePathToken(projectName), version.SanitizePathToken(ver))
		fmt.Fprintln(stdout, "📦 Creating SBOM ZIP archive...")
		if err := zipFiles(ws, zipName, matches); err != nil {
			fmt.Fprintf(stdout, "   ⚠️  Failed to create SBOM ZIP: %v\n", err)
			return nil
		}
		fmt.Fprintf(stdout, "✅ Created: %s\n\n", zipName)
		// Match the bash `unzip -l` listing.
		for _, m := range matches {
			info, _ := os.Stat(ws.outputPath(m))
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Fprintf(stdout, "  %8d  %s\n", size, m)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

// listSBOMs returns the SBOM files at the top of dir matching
// `*-sbom.*.json` (the bash `find -maxdepth 1` shape).
func listSBOMs(ws workspace, dir string) []string {
	entries, err := os.ReadDir(ws.outputPath(dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".json") && strings.Contains(name, "-sbom.") {
			out = append(out, name)
		}
	}
	return out
}

// zipFiles archives files into outputPath. Files are stored with their
// basename inside the archive (matches the bash `zip <archive> <file>`
// without `-r`).
func zipFiles(ws workspace, outputPath string, files []string) error {
	out, err := os.Create(ws.outputPath(outputPath))
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	w := zip.NewWriter(out)
	defer func() { _ = w.Close() }()
	for _, f := range files {
		if err := addFileToZip(ws, w, f); err != nil {
			return err
		}
	}
	return nil
}

func addFileToZip(ws workspace, w *zip.Writer, name string) error {
	src, err := os.Open(ws.outputPath(name))
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = path.Base(name)
	hdr.Method = zip.Deflate
	dst, err := w.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

// walkAllFiles returns every regular file under root as a slash-form
// relative path. Used by the build-BOM finder.
func walkAllFiles(ws workspace, root string) []string {
	var out []string
	_ = fs.WalkDir(ws.fsys, cleanFSPath(root), func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("walkAllFiles: skipping unreadable entry", "path", name, "err", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, relFromWalkRoot(root, name))
		return nil
	})
	return out
}
