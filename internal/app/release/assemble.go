// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	domainartifact "github.com/diggsweden/reusable-ci/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
	domainsbom "github.com/diggsweden/reusable-ci/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

const (
	assemblySourceBuildArtifact         = "build_artifact"
	assemblySourceExtractedBinaries     = "extracted_binaries"
	assemblySourceAttachment            = "attachment"
	assemblySourceGeneratedSBOM         = "generated_sbom"
	assemblySourceAnalyzedContainerSBOM = "analyzed_container_sbom"
)

// AssembleInput drives `reusable-ci release assemble`.
type AssembleInput struct {
	ConfigPlanJSON           string
	ArtifactTransferPlanJSON string
	AttachArtifacts          string
	ProjectName              string
	Version                  string
	OutputFile               string
	ReleaseFilesDir          string
	ReleaseArtifactsDir      string
	SBOMDir                  string
}

type assemblyCollector struct {
	assetsDir   string
	sbomsDir    string
	assets      []domainrelease.AssemblyFile
	sboms       []domainrelease.AssemblyFile
	assetByName map[string]domainrelease.AssemblyFile
	sbomByName  map[string]domainrelease.AssemblyFile
	seenSBOMSrc map[string]struct{}
}

// Assemble stages the final release files and writes the assembly manifest.
// It is deliberately forge-neutral: all provider/network work happens before
// this command via the artifact-transfer plan.
func Assemble(out io.Writer, in AssembleInput) (*domainrelease.ReleaseAssembly, error) {
	cfg, err := parseAssemblyConfigPlan(in.ConfigPlanJSON)
	if err != nil {
		return nil, err
	}

	transfer, err := parseAssemblyTransferPlan(in.ArtifactTransferPlanJSON)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(in.ProjectName) == "" {
		return nil, fmt.Errorf("project-name is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.Version) == "" {
		return nil, fmt.Errorf("version is required: %w", errs.ErrUsage)
	}

	outputFile := defaultString(in.OutputFile, domainrelease.DefaultAssemblyFile)
	releaseFilesDir := defaultString(in.ReleaseFilesDir, domainrelease.DefaultReleaseFilesDir)
	releaseArtifactsDir := defaultString(in.ReleaseArtifactsDir, domainrelease.DefaultReleaseArtifactsDir)
	sbomDir := defaultString(in.SBOMDir, domainrelease.DefaultSBOMArtifactsDir)

	if err := validateRelativeDir(releaseFilesDir, "release-files-dir"); err != nil {
		return nil, err
	}

	assetsDir := filepath.Join(releaseFilesDir, "assets")
	sbomsDir := filepath.Join(releaseFilesDir, "sboms")

	if err := recreateDir(assetsDir); err != nil {
		return nil, err
	}

	if err := recreateDir(sbomsDir); err != nil {
		return nil, err
	}

	collector := &assemblyCollector{
		assetsDir:   assetsDir,
		sbomsDir:    sbomsDir,
		assetByName: make(map[string]domainrelease.AssemblyFile),
		sbomByName:  make(map[string]domainrelease.AssemblyFile),
		seenSBOMSrc: make(map[string]struct{}),
	}

	if err := collectReleaseAssets(collector, cfg, transfer, releaseArtifactsDir); err != nil {
		return nil, err
	}

	if err := collectAttachmentAssets(collector, in.AttachArtifacts); err != nil {
		return nil, err
	}

	if err := collectAssemblySBOMs(collector, cfg, releaseFilesDir, releaseArtifactsDir, sbomDir); err != nil {
		return nil, err
	}

	sortAssemblyFiles(collector.assets)
	sortAssemblyFiles(collector.sboms)

	asm := &domainrelease.ReleaseAssembly{
		Version:      domainrelease.AssemblyVersion,
		Assets:       collector.assets,
		SBOMs:        collector.sboms,
		ChecksumFile: filepath.ToSlash(filepath.Join(releaseFilesDir, domainrelease.ChecksumsFile)),
		SBOMZipFile:  filepath.ToSlash(filepath.Join(assetsDir, domainsbom.ZipName(in.ProjectName, version.StripVPrefix(in.Version)))),
	}

	if err := writeAssembly(outputFile, asm); err != nil {
		return nil, err
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Assembled %d release asset(s) and %d SBOM input(s) in %s\n", len(asm.Assets), len(asm.SBOMs), outputFile)
	}

	return asm, nil
}

func parseAssemblyConfigPlan(raw string) (pipeline.ConfigPlan, error) {
	if strings.TrimSpace(raw) == "" {
		return pipeline.ConfigPlan{}, fmt.Errorf("config-plan-json is required: %w", errs.ErrUsage)
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return pipeline.ConfigPlan{}, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ConfigPlanVersion {
		return pipeline.ConfigPlan{}, fmt.Errorf("config-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	return plan, nil
}

func parseAssemblyTransferPlan(raw string) (pipeline.ArtifactTransferPlan, error) {
	if strings.TrimSpace(raw) == "" {
		return pipeline.ArtifactTransferPlan{}, fmt.Errorf("artifact-transfer-plan-json is required: %w", errs.ErrUsage)
	}

	var plan pipeline.ArtifactTransferPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return pipeline.ArtifactTransferPlan{}, fmt.Errorf("parse artifact-transfer-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ArtifactTransferPlanVersion {
		return pipeline.ArtifactTransferPlan{}, fmt.Errorf("artifact-transfer-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	for _, item := range plan.Items {
		if err := pipeline.ValidateArtifactTransferItem(item); err != nil {
			return pipeline.ArtifactTransferPlan{}, err
		}
	}

	return plan, nil
}

func collectReleaseAssets(c *assemblyCollector, cfg pipeline.ConfigPlan, transfer pipeline.ArtifactTransferPlan, dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat release artifacts dir %s: %w", dir, err)
	}

	hasNative := hasNativeArtifactFirst(cfg)
	rootAbs := absOrSelf(dir)

	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		if !entry.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if skipAssemblyReleaseFile(path, rel) {
			return nil
		}

		kind := assemblySourceBuildArtifact
		if firstPathSegment(rel) == "binaries" {
			kind = assemblySourceExtractedBinaries
		}

		if kind != assemblySourceExtractedBinaries && !isPlannedReleaseAsset(path, hasNative) {
			return nil
		}

		return c.addAsset(path, kind, transferNameForKind(transfer, kind, rootAbs, path), true)
	})
}

func collectAttachmentAssets(c *assemblyCollector, patterns string) error {
	parts := splitAttachPatterns(patterns)
	if len(parts) == 0 {
		return nil
	}

	for _, pattern := range parts {
		if filepath.IsAbs(pattern) {
			return fmt.Errorf("attach-artifacts pattern %q must be relative to the workspace: %w", pattern, errs.ErrValidation)
		}

		if strings.Contains(pattern, "..") {
			return fmt.Errorf("attach-artifacts pattern %q contains \"..\": %w", pattern, errs.ErrValidation)
		}
	}

	entries, err := domainartifact.CollectGlobEntries(parts, true)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := c.addAsset(entry.Abs, assemblySourceAttachment, "", true); err != nil {
			return err
		}
	}

	return nil
}

func collectAssemblySBOMs(c *assemblyCollector, cfg pipeline.ConfigPlan, releaseFilesDir, releaseArtifactsDir, sbomDir string) error {
	roots := plannedSBOMRoots(cfg)
	for _, root := range roots {
		if err := walkSBOMRoot(c, root, assemblySourceGeneratedSBOM, releaseFilesDir, releaseArtifactsDir); err != nil {
			return err
		}
	}

	return walkSBOMRoot(c, sbomDir, assemblySourceAnalyzedContainerSBOM, releaseFilesDir, releaseArtifactsDir)
}

func walkSBOMRoot(c *assemblyCollector, root, kind, releaseFilesDir, releaseArtifactsDir string) error {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat SBOM root %s: %w", root, err)
	}

	releaseFilesAbs := absOrSelf(releaseFilesDir)
	releaseArtifactsAbs := absOrSelf(releaseArtifactsDir)

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if shouldPruneSBOMDir(path, releaseFilesAbs, releaseArtifactsAbs) {
				return filepath.SkipDir
			}

			return nil
		}

		if !entry.Type().IsRegular() || !isAssemblySBOMFile(path) {
			return nil
		}

		return c.addSBOM(path, kind, "", false)
	})
}

func (c *assemblyCollector) addAsset(src, kind, sourceArtifact string, required bool) error {
	name := filepath.Base(src)
	if err := validateAssemblyName(name); err != nil {
		return err
	}

	abs := absOrSelf(src)
	if existing, ok := c.assetByName[name]; ok {
		if absOrSelf(existing.SourcePath) == abs {
			return nil
		}

		return fmt.Errorf("release asset basename %q is produced by both %s and %s: %w", name, existing.SourcePath, src, errs.ErrValidation)
	}

	dst := filepath.Join(c.assetsDir, name)
	if err := copyRegularFile(src, dst); err != nil {
		return err
	}

	file := domainrelease.AssemblyFile{
		Path:           filepath.ToSlash(dst),
		Name:           name,
		SourcePath:     filepath.ToSlash(src),
		SourceKind:     kind,
		SourceArtifact: sourceArtifact,
		Required:       required,
	}
	c.assetByName[name] = file
	c.assets = append(c.assets, file)

	return nil
}

func (c *assemblyCollector) addSBOM(src, kind, sourceArtifact string, required bool) error {
	abs := absOrSelf(src)
	if _, ok := c.seenSBOMSrc[abs]; ok {
		return nil
	}
	c.seenSBOMSrc[abs] = struct{}{}

	name := filepath.Base(src)
	if err := validateAssemblyName(name); err != nil {
		return err
	}

	if existing, ok := c.sbomByName[name]; ok {
		return fmt.Errorf("SBOM archive name %q is produced by both %s and %s: %w", name, existing.SourcePath, src, errs.ErrValidation)
	}

	dst := filepath.Join(c.sbomsDir, name)
	if err := copyRegularFile(src, dst); err != nil {
		return err
	}

	file := domainrelease.AssemblyFile{
		Path:           filepath.ToSlash(dst),
		Name:           name,
		SourcePath:     filepath.ToSlash(src),
		SourceKind:     kind,
		SourceArtifact: sourceArtifact,
		Required:       required,
	}
	c.sbomByName[name] = file
	c.sboms = append(c.sboms, file)

	return nil
}

func hasNativeArtifactFirst(cfg pipeline.ConfigPlan) bool {
	for _, artifact := range cfg.Artifacts.GoArtifactFirst {
		if artifact.BuildArtifactName != "" {
			return true
		}
	}

	for _, artifact := range cfg.Artifacts.CargoArtifactFirst {
		if artifact.BuildArtifactName != "" {
			return true
		}
	}

	return false
}

func plannedSBOMRoots(cfg pipeline.ConfigPlan) []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(cfg.Artifacts.All)+1)

	for _, artifact := range cfg.Artifacts.All {
		root := artifact.WorkingDirectory
		if root == "" {
			root = "."
		}

		if _, ok := seen[root]; ok {
			continue
		}

		seen[root] = struct{}{}
		roots = append(roots, root)
	}

	if len(roots) == 0 {
		roots = append(roots, ".")
	}

	sort.Strings(roots)

	return roots
}

func skipAssemblyReleaseFile(path, rel string) bool {
	base := filepath.Base(path)
	switch {
	case base == domainrelease.ChecksumsFile:
		return true
	case base == "release-images.json" || strings.HasPrefix(base, "release-images-ledger"):
		return true
	case base == "bom.json":
		return true
	case strings.HasSuffix(base, ".asc") || strings.HasSuffix(base, ".bundle"):
		return true
	case strings.HasPrefix(base, "original-") && strings.HasSuffix(base, ".jar"):
		return true
	case isAssemblySBOMFile(path):
		return true
	case strings.Contains(filepath.ToSlash(rel), "/.git/"):
		return true
	default:
		return false
	}
}

func isPlannedReleaseAsset(path string, hasNative bool) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)

	if domainrelease.IsReleaseArtifact(path) {
		return true
	}

	for _, ext := range []string{".apk", ".aab", ".ipa", ".exe"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	return hasNative && filepath.Ext(base) == ""
}

func isAssemblySBOMFile(path string) bool {
	base := filepath.Base(path)

	return strings.HasSuffix(base, "-sbom.spdx.json") || strings.HasSuffix(base, "-sbom.cyclonedx.json")
}

func shouldPruneSBOMDir(path, releaseFilesAbs, releaseArtifactsAbs string) bool {
	abs := absOrSelf(path)
	base := filepath.Base(path)

	return base == ".git" || abs == releaseFilesAbs || abs == releaseArtifactsAbs
}

func transferNameForKind(plan pipeline.ArtifactTransferPlan, kind, rootAbs, path string) string {
	for _, item := range plan.Items {
		if string(item.Kind) != kind {
			continue
		}

		itemRoot := absOrSelf(item.Path)
		if rootAbs != "" && itemRoot != rootAbs && !strings.HasPrefix(absOrSelf(path), itemRoot+string(filepath.Separator)) {
			continue
		}

		if item.Name != "" {
			return item.Name
		}

		return item.NameTemplate
	}

	return ""
}

func firstPathSegment(path string) string {
	path = filepath.ToSlash(path)
	if idx := strings.Index(path, "/"); idx >= 0 {
		return path[:idx]
	}

	return path
}

func validateAssemblyName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid release asset name %q: %w", name, errs.ErrValidation)
	}

	if !utf8.ValidString(name) {
		return fmt.Errorf("release asset name %q must be valid UTF-8: %w", name, errs.ErrValidation)
	}

	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("release asset name %q must not contain control characters: %w", name, errs.ErrValidation)
	}

	return nil
}

func copyRegularFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat %q: %w", src, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file: %w", src, errs.ErrValidation)
	}

	in, err := os.Open(src) //nolint:gosec // release assembly copies planned, workspace-local artifacts.
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec,mnd // public release staging dir.
		return fmt.Errorf("mkdir %q: %w", filepath.Dir(dst), err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec,mnd // public release asset.
	if err != nil {
		return fmt.Errorf("create %q: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy %q -> %q: %w", src, dst, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close %q: %w", dst, err)
	}

	return nil
}

func recreateDir(dir string) error {
	if err := validateRelativeDir(dir, "release staging dir"); err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %q: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec,mnd // public release staging dir.
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}

	return nil
}

func validateRelativeDir(dir, label string) error {
	clean := filepath.Clean(dir)
	if clean == "." || clean == string(filepath.Separator) || clean == "" || filepath.IsAbs(clean) {
		return fmt.Errorf("%s %q must be a relative non-root directory: %w", label, dir, errs.ErrValidation)
	}

	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return fmt.Errorf("%s %q must not traverse parent directories: %w", label, dir, errs.ErrValidation)
		}
	}

	return nil
}

func writeAssembly(path string, asm *domainrelease.ReleaseAssembly) error {
	body, err := json.MarshalIndent(asm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal release assembly: %w", err)
	}

	body = append(body, '\n')

	if path != cliio.StdSentinel {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // metadata dir under workspace.
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(path), err)
		}
	}

	return cliio.WriteFile(path, body, 0o644) //nolint:gosec // manifest is public release metadata.
}

func readAssembly(path string) (domainrelease.ReleaseAssembly, error) {
	body, err := cliio.ReadFile(path)
	if err != nil {
		return domainrelease.ReleaseAssembly{}, fmt.Errorf("read release assembly %s: %w", path, err)
	}

	var asm domainrelease.ReleaseAssembly
	if err := json.Unmarshal(body, &asm); err != nil {
		return domainrelease.ReleaseAssembly{}, fmt.Errorf("parse release assembly %s: %w: %w", path, err, errs.ErrInvalidConfig)
	}

	if asm.Version != domainrelease.AssemblyVersion {
		return domainrelease.ReleaseAssembly{}, fmt.Errorf("release assembly has unsupported version %d: %w", asm.Version, errs.ErrInvalidConfig)
	}

	return asm, nil
}

func sortAssemblyFiles(files []domainrelease.AssemblyFile) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}

func regularFileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular() && info.Size() > 0
}
