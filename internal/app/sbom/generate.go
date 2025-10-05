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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	defer func() { _ = ws.close() }()

	layers := in.Layers
	if layers == "" {
		layers = "build"
	}

	parsedLayers := domainsbom.ParseLayerCSV(layers)
	if len(parsedLayers) == 0 {
		return fmt.Errorf("at least one SBOM layer is required: %w", errs.ErrUsage)
	}

	if in.ProjectType != "" && !domainsbom.IsValidProjectType(in.ProjectType) {
		parseErr := (&projecttype.UnknownTypeError{Input: in.ProjectType, Valid: domainsbom.ValidProjectTypes}).Error()

		return fmt.Errorf("invalid --project-type %q: %s: %w", in.ProjectType, parseErr, errs.ErrUsage)
	}

	emitGenerateHeader(w, ws.root, in.ProjectType, layers)

	projectType := resolveProjectType(ws, in.ProjectType, w)

	resolvedName, resolvedVersion, err := resolveNameAndVersion(ctx, ws, mvn, projectType, in.Name, in.Version)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "Project Information:")
	_, _ = fmt.Fprintf(w, "  Name: %s\n", resolvedName)
	_, _ = fmt.Fprintf(w, "  Version: %s\n", resolvedVersion)
	_, _ = fmt.Fprintf(w, "  Type: %s\n\n", projectType)

	// Short SHA for filename traceability (best-effort).
	sha, _ := gitRepo.Run(ctx, "rev-parse", "--short", "HEAD")
	sha = strings.TrimSpace(sha)

	// Sanitised here, once: every layer filename is derived from these, so the
	// subject that flows down is already path-safe.
	subj := subject{
		name:    version.SanitizePathToken(resolvedName),
		version: version.SanitizePathToken(resolvedVersion),
		sha:     sha,
	}

	deps := layerDeps{ws: ws, syft: syft, gen: gen, w: w, stderr: stderr}

	if err := generateLayers(ctx, deps, parsedLayers, projectType, subj, in.ContainerImage); err != nil {
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

func resolveNameAndVersion(ctx context.Context, ws workspace, mvn MavenOps, projectType projecttype.Type, nameIn, versionIn string) (string, string, error) {
	resolvedVersion := versionIn
	if resolvedVersion == "" {
		if projectType == projecttype.Cargo {
			var err error

			resolvedVersion, err = readCargoVersion(ws)
			if err != nil {
				return "", "", err
			}
		} else {
			resolvedVersion = readVersion(ctx, ws, mvn, projectType)
		}
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

	return resolvedName, resolvedVersion, nil
}

func readCargoVersion(ws workspace) (string, error) { //nolint:cyclop // Bounded ancestor discovery keeps Cargo workspace resolution explicit.
	body, err := ws.readFile("Cargo.toml")
	if err != nil {
		return "", nil //nolint:nilerr // A missing/unreadable project manifest retains the existing unknown-version fallback.
	}

	resolved, resolveErr := domainsbom.CargoTOMLVersion(body, body)
	if resolveErr == nil {
		return resolved, nil
	}

	if !errors.Is(resolveErr, domainsbom.ErrCargoWorkspaceVersionUnavailable) {
		return "", fmt.Errorf("resolve Cargo package version: %w", resolveErr)
	}

	if !ws.hostFS {
		return "", fmt.Errorf("resolve Cargo package version in supplied filesystem: %w", resolveErr)
	}

	for dir := filepath.Dir(ws.root); ; dir = filepath.Dir(dir) {
		workspaceBody, readErr := os.ReadFile(filepath.Join(dir, "Cargo.toml")) //nolint:gosec // Candidate is the fixed Cargo.toml name under bounded workspace ancestors.
		if readErr == nil {
			resolved, candidateErr := domainsbom.CargoTOMLVersion(body, workspaceBody)
			if candidateErr == nil {
				return resolved, nil
			}

			if !errors.Is(candidateErr, domainsbom.ErrCargoWorkspaceVersionUnavailable) {
				return "", fmt.Errorf("resolve Cargo package version from %s: %w", filepath.Join(dir, "Cargo.toml"), candidateErr)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return "", fmt.Errorf("read possible Cargo workspace manifest %s: %w", filepath.Join(dir, "Cargo.toml"), readErr)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return "", fmt.Errorf("resolve Cargo package version in %s; no current or ancestor Cargo.toml defines [workspace.package].version: %w", ws.root, resolveErr)
}

// generateLayers fans the parsed-layer CSV out to the per-layer
// generators. An unknown layer name surfaces as both a warning line and
// a typed error from the domain (UnknownLayerError).
func generateLayers(
	ctx context.Context,
	deps layerDeps,
	parsedLayers []string,
	projectType projecttype.Type,
	subj subject,
	containerImage string,
) error {
	for _, layer := range parsedLayers {
		if !domainsbom.IsValidLayer(layer) {
			_, _ = fmt.Fprintf(deps.w, "   ⚠️  Unknown layer: %s (valid: build, analyzed-artifact, analyzed-container)\n", layer)

			return domainsbom.UnknownLayerError(layer)
		}

		switch domainsbom.LayerName(layer) {
		case domainsbom.LayerBuild:
			if err := generateBuildLayer(ctx, deps, projectType, subj); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedArtifact:
			if err := generateArtifactLayer(ctx, deps, projectType, subj); err != nil {
				return err
			}
		case domainsbom.LayerAnalyzedContainer:
			if err := generateContainerLayer(ctx, deps, containerImage, subj); err != nil {
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

	matches, err := listSBOMs(ws, ".")
	if err != nil {
		return err
	}

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
		info, err := ws.outputInfo(m)
		if err != nil {
			return fmt.Errorf("stat generated SBOM %s: %w", m, err)
		}

		_, _ = fmt.Fprintf(w, "  %8d  %s\n", info.Size(), m)
	}

	_, _ = fmt.Fprintln(w)

	if createZip {
		zipName := domainsbom.ZipName(version.SanitizePathToken(projectName), version.SanitizePathToken(ver))

		_, _ = fmt.Fprintln(w, "📦 Creating SBOM ZIP archive...")

		if err := zipFiles(ws, zipName, matches); err != nil {
			_, _ = fmt.Fprintf(w, "   ⚠️  Failed to create SBOM ZIP: %v\n", err)

			return fmt.Errorf("create SBOM ZIP %s: %w", zipName, err)
		}

		_, _ = fmt.Fprintf(w, "%s Created: %s\n\n", clicolor.Check(w), zipName)
		// Match the bash `unzip -l` listing.
		for _, m := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			info, err := ws.outputInfo(m)
			if err != nil {
				return fmt.Errorf("stat archived SBOM %s: %w", m, err)
			}

			_, _ = fmt.Fprintf(w, "  %8d  %s\n", info.Size(), m)
		}

		_, _ = fmt.Fprintln(w)
	}

	return nil
}

// listSBOMs returns the SBOM files at the top of dir matching
// `*-sbom.*.json` (the bash `find -maxdepth 1` shape).
func listSBOMs(ws workspace, dir string) ([]string, error) {
	entries, err := ws.outputEntries(dir)
	if err != nil {
		return nil, fmt.Errorf("list generated SBOMs in %s: %w", dir, err)
	}

	var out []string

	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") && strings.Contains(name, "-sbom.") {
			info, infoErr := e.Info()
			if infoErr != nil {
				return nil, fmt.Errorf("inspect generated SBOM %s: %w", name, infoErr)
			}

			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("generated SBOM is not a regular file: %s: %w", name, errs.ErrValidation)
			}

			out = append(out, name)
		}
	}

	return out, nil
}

// zipFiles archives files into outputPath. Files are stored with their
// basename inside the archive.
func zipFiles(ws workspace, outputPath string, files []string) error {
	out, err := ws.createOutput(outputPath, 0o600)
	if err != nil {
		return err
	}

	zipWriter := zip.NewWriter(out)

	complete := false
	defer func() {
		if !complete {
			_ = zipWriter.Close()
			_ = out.Close()
			_ = ws.removeOutput(outputPath)
		}
	}()

	for _, f := range files {
		if err := addFileToZip(ws, zipWriter, f); err != nil {
			return err
		}
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("finalize ZIP: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close ZIP: %w", err)
	}

	complete = true

	return nil
}

func addFileToZip(ws workspace, w *zip.Writer, name string) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	src, err := ws.openOutputRegular(name)
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
func walkAllFiles(ws workspace, root string) ([]string, error) {
	if err := validateWalkRoot(ws, root); err != nil {
		return nil, err
	}

	var out []string

	err := fs.WalkDir(ws.fsys, cleanFSPath(root), func(name string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			out = append(out, relFromWalkRoot(root, name))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk build SBOM inputs under %s: %w", root, err)
	}

	return out, nil
}
