// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainsbom "github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// pomFieldSelector picks which ParsePOM field readMavenPOMField returns.
type pomFieldSelector int

const (
	mavenPOMVersion pomFieldSelector = iota
	mavenPOMArtifactID
)

// readMavenPOMField parses pom.xml from the workspace and returns the
// requested field. Empty string on any failure — the caller decides
// whether to fall back to `mvn help:evaluate`.
func readMavenPOMField(ws workspace, field pomFieldSelector) string {
	body, err := ws.readFile("pom.xml")
	if err != nil {
		return ""
	}

	pom, err := domainbuild.ParsePOM(body)
	if err != nil {
		return ""
	}

	switch field {
	case mavenPOMVersion:
		return pom.Version
	case mavenPOMArtifactID:
		return pom.ArtifactID
	}

	return ""
}

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

// BuildSBOMGenerator produces a build-layer CycloneDX bom.json on demand, for
// ecosystems whose SBOM tool reads the lockfile (go, cargo) and so needs no
// compiled artifact. It lets `sbom assemble` GENERATE the build layer when no
// build byproduct exists, not just harvest one — dissolving the old split where
// go used `build go sbom` and cargo shelled out to `cargo cyclonedx` inline.
// Ecosystems that emit the BOM only as a build byproduct (maven/gradle/npm)
// return errs.ErrUnsupported and are harvested instead. The implementation is
// injected at the CLI (it wraps the build adapters), keeping app/sbom free of
// app/build and adapter imports.
type BuildSBOMGenerator interface {
	GenerateBuildSBOM(ctx context.Context, projectType projecttype.Type, dir, name string, stderr io.Writer) error
}

// Generate orchestrates the reusable-ci SBOM generation pipeline end-to-end.
func Generate(
	ctx context.Context,
	syft SyftOps,
	mvn MavenOps,
	gitRepo GitOps,
	gen BuildSBOMGenerator,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
		parseErr := (&projecttype.UnknownTypeError{Input: in.ProjectType, Valid: domainsbom.ValidProjectTypes}).Error()

		return fmt.Errorf("invalid --project-type %q: %s: %w", in.ProjectType, parseErr, errs.ErrUsage)
	}

	emitGenerateHeader(w, ws.root, in.ProjectType, layers)

	projectType := resolveProjectType(ws, in.ProjectType, w)
	resolvedName, resolvedVersion := resolveNameAndVersion(ctx, ws, mvn, projectType, in.Name, in.Version)

	_, _ = fmt.Fprintln(w, "Project Information:")
	_, _ = fmt.Fprintf(w, "  Name: %s\n", resolvedName)
	_, _ = fmt.Fprintf(w, "  Version: %s\n", resolvedVersion)
	_, _ = fmt.Fprintf(w, "  Type: %s\n\n", projectType)

	// Short SHA for filename traceability (best-effort).
	sha, _ := gitRepo.Run(ctx, "rev-parse", "--short", "HEAD")
	sha = strings.TrimSpace(sha)

	safeName := version.SanitizePathToken(resolvedName)
	safeVersion := version.SanitizePathToken(resolvedVersion)

	if err := generateLayers(ctx, ws, syft, gen, parsedLayers, projectType, safeName, safeVersion, sha, in.ContainerImage, w, stderr); err != nil {
		return err
	}

	return generateSummary(w, ws, resolvedName, resolvedVersion, in.CreateZip)
}

func emitGenerateHeader(w io.Writer, root, projectTypeIn, layers string) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintln(w, "================================================")
	_, _ = fmt.Fprintln(w, "SBOM Assembly")
	_, _ = fmt.Fprintln(w, "================================================")
	_, _ = fmt.Fprintf(w, "Working directory: %s\n", root)
	_, _ = fmt.Fprintf(w, "Project type: %s\n", cmp.Or(projectTypeIn, string(projecttype.Auto)))
	_, _ = fmt.Fprintf(w, "Requested layers: %s\n\n", layers)
}

func resolveProjectType(ws workspace, projectTypeIn string, w io.Writer) projecttype.Type { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	projectType := projecttype.Type(projectTypeIn)
	if projectType != "" && projectType != projecttype.Auto {
		return projectType
	}

	entries, _ := ws.readDir(".")

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}

	projectType = domainsbom.DetectProjectType(names)
	_, _ = fmt.Fprintf(w, "Auto-detected project type: %s\n", projectType)

	return projectType
}

func resolveNameAndVersion(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type, nameIn, versionIn string) (string, string) {
	resolvedVersion := versionIn
	if resolvedVersion == "" {
		resolvedVersion = readVersion(ctx, ws, mvn, projectType)
	}

	if resolvedVersion == "" {
		resolvedVersion = "unknown"
	}

	resolvedName := nameIn
	if resolvedName == "" {
		resolvedName = readName(ctx, ws, mvn, projectType)
	}

	if resolvedName == "" {
		resolvedName = ws.base()
	}

	return resolvedName, resolvedVersion
}

// generateLayers fans the parsed-layer CSV out to the per-layer
// generators. An unknown layer name surfaces as both a warning line and
// a typed error from the domain (UnknownLayerError).
func generateLayers(
	ctx context.Context,
	ws workspace,
	syft SyftOps,
	gen BuildSBOMGenerator,
	parsedLayers []string,
	projectType projecttype.Type,
	safeName, safeVersion, sha, containerImage string,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
) error {
	for _, layer := range parsedLayers {
		if !domainsbom.IsValidLayer(layer) {
			_, _ = fmt.Fprintf(w, "   ⚠️  Unknown layer: %s (valid: build, analyzed-artifact, analyzed-container)\n", layer)

			return domainsbom.UnknownLayerError(layer)
		}

		switch domainsbom.LayerName(layer) {
		case domainsbom.LayerBuild:
			if err := generateBuildLayer(ctx, ws, gen, projectType, safeName, safeVersion, sha, w, stderr); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedArtifact:
			if err := generateArtifactLayer(ctx, ws, syft, projectType, safeName, safeVersion, sha, w, stderr); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedContainer:
			if err := generateContainerLayer(ctx, ws, syft, containerImage, safeName, safeVersion, sha, w, stderr); err != nil {
				return err
			}
		}
	}

	return nil
}

// readVersion fetches the project version per project type. Errors are
// swallowed — the caller falls back to "unknown".
//
// Parallel to readName by design; per-ecosystem branches read distinct
// files / call distinct domain functions, so abstracting the shared
// shape behind a table-driven helper would obscure rather than clarify.
//
//nolint:dupl,cyclop // parallel to readName; version-source dispatch with one branch per project type.
func readVersion(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type) string {
	switch projectType {
	case projecttype.Maven:
		if v := readMavenPOMField(ws, mavenPOMVersion); v != "" { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if domainbuild.POMHasUnresolvedProperty(v) && mvn != nil {
				expanded, _ := mvn.EvalExpression(ctx, "project.version")

				return strings.TrimSpace(expanded)
			}

			return v
		}

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
	default:
		// Auto / GradleAndroid / XcodeIOS / Meta / Unknown: no in-process
		// version reader — the caller falls back to "unknown".
	}

	return ""
}

// readName fetches the project name per project type.
//
//nolint:dupl,cyclop // parallel to readVersion; name-source dispatch with one branch per project type.
func readName(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type) string {
	switch projectType {
	case projecttype.Maven:
		if v := readMavenPOMField(ws, mavenPOMArtifactID); v != "" { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if domainbuild.POMHasUnresolvedProperty(v) && mvn != nil {
				expanded, _ := mvn.EvalExpression(ctx, "project.artifactId")

				return strings.TrimSpace(expanded)
			}

			return v
		}

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
	default:
		// Auto / GradleAndroid / XcodeIOS / Meta / Unknown: no in-process
		// name reader — the caller falls back to the directory basename.
	}

	return ""
}

// generateSummary mirrors the `generate_summary` function: lists the
// SBOMs produced in the working dir, optionally zips them.
func generateSummary(w io.Writer, ws workspace, projectName, ver string, createZip bool) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintln(w, "================================================")
	_, _ = fmt.Fprintln(w, "SBOM Assembly Complete")
	_, _ = fmt.Fprintln(w, "================================================")

	matches := listSBOMs(ws, ".")
	if len(matches) == 0 {
		// Name the fix — `assemble` runs AFTER the build, so an empty result
		// almost always means a missing input rather than a tool failure. The
		// concise wrapped error carries the exit code; this hint carries the why.
		_, _ = fmt.Fprintln(w, "No SBOM layers were produced. `sbom assemble` runs AFTER the build:")
		_, _ = fmt.Fprintln(w, "  • build              — harvests the BOM your build emits (cyclonedx-gomod / build-go.yml); run that first.")
		_, _ = fmt.Fprintln(w, "  • analyzed-artifact  — syft-scans a built artifact; build one first.")
		_, _ = fmt.Fprintln(w, "  • analyzed-container — syft-scans an image; pass --container-image.")

		return fmt.Errorf("no SBOM layers produced: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "%s Successfully generated %d SBOM files\n\n", clicolor.Check(w), len(matches))
	_, _ = fmt.Fprintln(w, "Generated files:")

	for _, m := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		info, _ := os.Stat(ws.outputPath(m))

		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		_, _ = fmt.Fprintf(w, "  %8d  %s\n", size, m)
	}

	_, _ = fmt.Fprintln(w)

	if createZip {
		zipName := domainsbom.ZipName(version.SanitizePathToken(projectName), version.SanitizePathToken(ver))

		_, _ = fmt.Fprintln(w, "📦 Creating SBOM ZIP archive...")

		if err := zipFiles(ws, zipName, matches); err != nil {
			_, _ = fmt.Fprintf(w, "   ⚠️  Failed to create SBOM ZIP: %v\n", err)

			return nil
		}

		_, _ = fmt.Fprintf(w, "%s Created: %s\n\n", clicolor.Check(w), zipName)
		// Match the bash `unzip -l` listing.
		for _, m := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			info, _ := os.Stat(ws.outputPath(m))

			size := int64(0)
			if info != nil {
				size = info.Size()
			}

			_, _ = fmt.Fprintf(w, "  %8d  %s\n", size, m)
		}

		_, _ = fmt.Fprintln(w)
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
// basename inside the archive.
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

func addFileToZip(ws workspace, w *zip.Writer, name string) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	_ = fs.WalkDir(ws.fsys, cleanFSPath(root), func(name string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("walkAllFiles: skipping unreadable entry", "path", name, "err", err)

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
