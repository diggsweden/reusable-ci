// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// Release file manifest defaults shared by the `release files` commands.
const (
	// DefaultReleaseFilesManifest is the default release file manifest path.
	DefaultReleaseFilesManifest = "dist/release-files.json"
	defaultReleaseDistDir       = "dist"
	releaseFilesVersion         = 1
	releaseFilesSectionAssets   = "assets"
)

// FileEntry is one classified release file in the manifest.
type FileEntry struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// FileAssets is the `release files collect` output: the versioned
// list of publishable release assets.
type FileAssets struct {
	Version int         `json:"version"`
	Assets  []FileEntry `json:"assets"`
}

// FileManifest is the versioned release file manifest written by
// WriteReleaseFileManifest and consumed by the sign/publish steps.
type FileManifest struct {
	Version    int         `json:"version"`
	Assets     []FileEntry `json:"assets"`
	Checksums  []FileEntry `json:"checksums"`
	SBOMs      []FileEntry `json:"sboms"`
	Evidence   []FileEntry `json:"evidence"`
	Provenance []FileEntry `json:"provenance"`
}

// FilesInput is the shared dist-dir/manifest pair driving the
// `release files` commands.
type FilesInput struct {
	DistDir      string
	ManifestFile string
}

// WriteReleaseFileManifestInput drives WriteReleaseFileManifest.
type WriteReleaseFileManifestInput struct {
	FilesInput
	OutputFile     string
	AssetsJSONFile string
}

// ListReleaseFilesInput drives ListReleaseFiles.
type ListReleaseFilesInput struct {
	FilesInput
	Section string
}

// ValidateReleaseChecksumsInput drives ValidateReleaseChecksums.
type ValidateReleaseChecksumsInput struct {
	FilesInput
	ChecksumsFile string
}

// CollectReleaseAssets discovers the publishable release assets from
// GoReleaser metadata and signed sidecars under the dist directory.
func CollectReleaseAssets(in FilesInput) (*FileAssets, error) {
	ctx := releaseFilesContext(in)

	assets, err := collectReleaseAssetEntries(ctx)
	if err != nil {
		return nil, err
	}

	return &FileAssets{Version: releaseFilesVersion, Assets: assets}, nil
}

// WriteReleaseFileManifest writes (and re-validates) the versioned release
// file manifest, collecting assets when no pre-collected JSON is supplied.
func WriteReleaseFileManifest(in WriteReleaseFileManifestInput) (*FileManifest, error) {
	ctx := releaseFilesContext(in.FilesInput)

	output := strings.TrimSpace(in.OutputFile)
	if output == "" {
		output = ctx.manifestFile
	}

	assets, err := resolveManifestAssets(ctx, in.AssetsJSONFile)
	if err != nil {
		return nil, err
	}

	manifest, err := buildReleaseFileManifest(ctx, assets)
	if err != nil {
		return nil, err
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal release file manifest: %w", err)
	}

	body = append(body, '\n')

	if output != cliio.StdSentinel {
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil { //nolint:gosec,mnd // release metadata under workspace.
			return nil, fmt.Errorf("create release file manifest directory: %w", err)
		}
	}

	if err := cliio.WriteFile(output, body, 0o644); err != nil { //nolint:gosec // public release metadata.
		return nil, fmt.Errorf("write release file manifest %s: %w", output, err)
	}

	if err := validateReleaseFileManifest(ctx, manifest); err != nil {
		return nil, err
	}

	return manifest, nil
}

// resolveManifestAssets returns the pre-collected assets from assetsJSONFile
// when supplied and collects them from the dist directory otherwise.
func resolveManifestAssets(ctx releaseFilesCtx, assetsJSONFile string) ([]FileEntry, error) {
	if assetsJSONFile != "" {
		return readReleaseAssetsJSON(assetsJSONFile)
	}

	return collectReleaseAssetEntries(ctx)
}

// ValidateReleaseFileManifest reads the release file manifest and checks
// every structural invariant (version, entry shape, classification).
func ValidateReleaseFileManifest(in FilesInput) error {
	ctx := releaseFilesContext(in)

	manifest, err := readReleaseFileManifest(ctx.manifestFile)
	if err != nil {
		return err
	}

	return validateReleaseFileManifest(ctx, manifest)
}

// ListReleaseFiles prints the release file paths for one manifest section,
// falling back to on-disk discovery when no manifest exists yet.
func ListReleaseFiles(in ListReleaseFilesInput) ([]string, error) {
	ctx := releaseFilesContext(in.FilesInput)

	section := strings.TrimSpace(in.Section)
	if section == "" {
		section = releaseFilesSectionAssets
	}

	if releaseFileManifestAvailable(ctx.manifestFile) {
		return listManifestSectionPaths(ctx, section)
	}

	return discoverReleaseFilesSection(ctx, section)
}

// listManifestSectionPaths lists one section of a validated release file
// manifest that is already known to exist.
func listManifestSectionPaths(ctx releaseFilesCtx, section string) ([]string, error) {
	manifest, err := readReleaseFileManifest(ctx.manifestFile)
	if err != nil {
		return nil, err
	}

	if validateErr := validateReleaseFileManifest(ctx, manifest); validateErr != nil {
		return nil, validateErr
	}

	entries, err := manifestSection(manifest, section)
	if err != nil {
		return nil, err
	}

	return entryPaths(entries), nil
}

// discoverReleaseFilesSection lists one release files section by on-disk
// discovery when no manifest has been written yet.
func discoverReleaseFilesSection(ctx releaseFilesCtx, section string) ([]string, error) {
	switch section {
	case releaseFilesSectionAssets:
		return discoverAssetSectionPaths(ctx)
	case "checksums":
		return discoverChecksumSectionPaths(ctx)
	case "sboms":
		return discoverSBOMSectionPaths(ctx)
	case "evidence":
		return nil, nil
	case "provenance":
		return discoverProvenanceSectionPaths(ctx), nil
	default:
		return nil, fmt.Errorf("release files section must be one of assets, checksums, sboms, evidence, provenance (got %q): %w", section, errs.ErrUsage)
	}
}

// discoverAssetSectionPaths lists the public release asset paths found on disk.
func discoverAssetSectionPaths(ctx releaseFilesCtx) ([]string, error) {
	assets, err := collectReleaseAssetEntries(ctx)
	if err != nil {
		return nil, err
	}

	return entryPaths(assets), nil
}

// discoverChecksumSectionPaths lists the single on-disk checksums file.
func discoverChecksumSectionPaths(ctx releaseFilesCtx) ([]string, error) {
	checksum, err := discoverSingleChecksum(ctx)
	if err != nil {
		return nil, err
	}

	return []string{checksum}, nil
}

// discoverSBOMSectionPaths lists the dist SBOM paths found on disk.
func discoverSBOMSectionPaths(ctx releaseFilesCtx) ([]string, error) {
	sboms, err := discoverDistSBOMEntries(ctx)
	if err != nil {
		return nil, err
	}

	return entryPaths(sboms), nil
}

// discoverProvenanceSectionPaths lists the provenance envelope when present.
func discoverProvenanceSectionPaths(ctx releaseFilesCtx) []string {
	if !regularReleaseFile(ctx.provenancePath()) {
		return nil
	}

	return []string{ctx.provenancePath()}
}

// FindReleaseChecksumFile returns the single release checksums file, from the
// manifest when available and by dist-dir discovery otherwise.
func FindReleaseChecksumFile(in FilesInput) (string, error) {
	ctx := releaseFilesContext(in)
	if releaseFileManifestAvailable(ctx.manifestFile) {
		manifest, err := readReleaseFileManifest(ctx.manifestFile)
		if err != nil {
			return "", err
		}

		if err := validateReleaseFileManifest(ctx, manifest); err != nil {
			return "", err
		}

		if len(manifest.Checksums) != 1 {
			return "", fmt.Errorf("expected exactly one checksums file in release file manifest, found %d: %w", len(manifest.Checksums), errs.ErrValidation)
		}

		return manifest.Checksums[0].Path, nil
	}

	return discoverSingleChecksum(ctx)
}

// ValidateReleaseChecksums checks that the checksums file names every public
// release asset exactly once and nothing else.
func ValidateReleaseChecksums(in ValidateReleaseChecksumsInput) error {
	ctx := releaseFilesContext(in.FilesInput)

	checksumFile := strings.TrimSpace(in.ChecksumsFile)
	if checksumFile == "" {
		var err error

		checksumFile, err = FindReleaseChecksumFile(in.FilesInput)
		if err != nil {
			return err
		}
	}

	subjects, err := parseChecksumFileSubjects(checksumFile)
	if err != nil {
		return err
	}

	allowed, err := allowedChecksumSubjects(ctx)
	if err != nil {
		return err
	}

	return checkChecksumSubjects(subjects, allowed)
}

// parseChecksumFileSubjects reads the checksums file and parses its subject
// lines.
func parseChecksumFileSubjects(checksumFile string) ([]provenance.Subject, error) {
	if !regularReleaseFile(checksumFile) {
		return nil, fmt.Errorf("checksum file is missing, not a regular file, or a symlink: %s: %w", checksumFile, errs.ErrMissingInput)
	}

	body, err := os.Open(checksumFile) //nolint:gosec // operator-supplied release metadata path.
	if err != nil {
		return nil, fmt.Errorf("open checksum file %s: %w", checksumFile, err)
	}

	defer func() { _ = body.Close() }()

	return provenance.ParseChecksums(body)
}

// checkChecksumSubjects verifies that the parsed subjects name every allowed
// release asset exactly once and nothing else.
func checkChecksumSubjects(subjects []provenance.Subject, allowed []string) error {
	subjectCounts := make(map[string]int, len(subjects))
	for _, subject := range subjects {
		subjectCounts[subject.Name]++
	}

	duplicates := sortedKeysMatching(subjectCounts, func(_ string, count int) bool { return count > 1 })
	if len(duplicates) > 0 {
		return fmt.Errorf("checksum file contains duplicate subjects: %v: %w", duplicates, errs.ErrValidation)
	}

	subjectNames := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		subjectNames = append(subjectNames, subject.Name)
	}

	unknown := namesNotIn(subjectNames, allowed)
	if len(unknown) > 0 {
		return fmt.Errorf("checksum file references files that are not public release assets: %v: %w", unknown, errs.ErrValidation)
	}

	missing := namesNotIn(allowed, subjectNames)
	if len(missing) > 0 {
		return fmt.Errorf("checksum file is missing public release assets: %v: %w", missing, errs.ErrValidation)
	}

	return nil
}

// namesNotIn returns the sorted unique values that do not appear in others.
func namesNotIn(values, others []string) []string {
	otherSet := stringSet(others)

	var out []string

	for _, value := range values {
		if _, ok := otherSet[value]; !ok {
			out = append(out, value)
		}
	}

	return uniqueSorted(out)
}

type releaseFilesCtx struct {
	distDir      string
	manifestFile string
}

func releaseFilesContext(in FilesInput) releaseFilesCtx {
	distDir := strings.TrimSpace(in.DistDir)
	if distDir == "" {
		distDir = defaultReleaseDistDir
	}

	manifest := strings.TrimSpace(in.ManifestFile)
	if manifest == "" {
		manifest = DefaultReleaseFilesManifest
	}

	return releaseFilesCtx{distDir: filepath.Clean(distDir), manifestFile: manifest}
}

func (c releaseFilesCtx) artifactsJSONPath() string {
	return filepath.Join(c.distDir, "artifacts.json")
}

func (c releaseFilesCtx) provenancePath() string {
	return filepath.Join(c.distDir, "slsa-provenance.intoto.json")
}

func collectReleaseAssetEntries(ctx releaseFilesCtx) ([]FileEntry, error) {
	artifacts, err := readGoReleaserArtifacts(ctx)
	if err != nil {
		return nil, err
	}

	collector := &releaseFileCollector{}

	if err := addGoReleaserAssetEntries(ctx, collector, artifacts); err != nil {
		return nil, err
	}

	if err := addProvenanceAssetEntries(ctx, collector); err != nil {
		return nil, err
	}

	if err := addSignatureBundleEntries(ctx, collector); err != nil {
		return nil, err
	}

	if err := addPackageSignatureEntries(ctx, collector); err != nil {
		return nil, err
	}

	if len(collector.entries) == 0 {
		return nil, fmt.Errorf("no release assets found in %s: %w", ctx.artifactsJSONPath(), errs.ErrMissingInput)
	}

	return collector.entries, nil
}

func addGoReleaserAssetEntries(ctx releaseFilesCtx, collector *releaseFileCollector, artifacts []goReleaserArtifact) error {
	for _, artifact := range artifacts {
		if publishableGoReleaserAsset(artifact.Path, artifact.Type) {
			if err := collector.add(ctx, artifact.Path, "goreleaser"); err != nil {
				return err
			}
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Skipping non-release GoReleaser artifact: %s\n", artifact.Path)
		}
	}

	return nil
}

func addProvenanceAssetEntries(ctx releaseFilesCtx, collector *releaseFileCollector) error {
	if err := collector.add(ctx, ctx.provenancePath(), "provenance"); err != nil {
		return err
	}

	return collector.add(ctx, ctx.provenancePath()+".bundle", "provenance_bundle")
}

func addSignatureBundleEntries(ctx releaseFilesCtx, collector *releaseFileCollector) error {
	checksumFile, err := discoverSingleChecksum(ctx)
	if err == nil && regularReleaseFile(checksumFile+".bundle") {
		if addErr := collector.add(ctx, checksumFile+".bundle", "signature_bundle"); addErr != nil {
			return addErr
		}
	}

	sboms, err := discoverDistSBOMEntries(ctx)
	if err != nil {
		return err
	}

	for _, sbom := range sboms {
		bundle := sbom.Path + ".bundle"
		if regularReleaseFile(bundle) {
			if err := collector.add(ctx, bundle, "signature_bundle"); err != nil {
				return err
			}
		}
	}

	return nil
}

func addPackageSignatureEntries(ctx releaseFilesCtx, collector *releaseFileCollector) error {
	for _, pattern := range []string{filepath.Join(ctx.distDir, "*.deb.sig"), filepath.Join(ctx.distDir, "*.rpm.sig"), filepath.Join(ctx.distDir, "*.apk.sig")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("glob release package signatures %q: %w", pattern, err)
		}

		for _, match := range matches {
			if err := collector.add(ctx, match, "package_signature"); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildReleaseFileManifest(ctx releaseFilesCtx, assets []FileEntry) (*FileManifest, error) {
	checksumFile, err := discoverSingleChecksum(ctx)
	if err != nil {
		return nil, err
	}

	checksums, err := entriesFromPaths(ctx, "checksum", []string{checksumFile})
	if err != nil {
		return nil, err
	}

	sboms, err := discoverDistSBOMEntries(ctx)
	if err != nil {
		return nil, err
	}

	var provenanceEntries []FileEntry
	if regularReleaseFile(ctx.provenancePath()) {
		provenanceEntries, err = entriesFromPaths(ctx, "provenance", []string{ctx.provenancePath()})
		if err != nil {
			return nil, err
		}
	}

	manifest := &FileManifest{
		Version:    releaseFilesVersion,
		Assets:     assets,
		Checksums:  checksums,
		SBOMs:      sboms,
		Evidence:   []FileEntry{},
		Provenance: provenanceEntries,
	}

	return manifest, nil
}

type goReleaserArtifact struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func readGoReleaserArtifacts(ctx releaseFilesCtx) ([]goReleaserArtifact, error) {
	body, err := os.ReadFile(ctx.artifactsJSONPath()) //nolint:gosec // release metadata path under workspace.
	if err != nil {
		return nil, fmt.Errorf("read GoReleaser artifacts %s: %w", ctx.artifactsJSONPath(), err)
	}

	var artifacts []goReleaserArtifact
	if err := json.Unmarshal(body, &artifacts); err != nil {
		return nil, fmt.Errorf("parse GoReleaser artifacts %s: %w: %w", ctx.artifactsJSONPath(), err, errs.ErrInvalidConfig)
	}

	return artifacts, nil
}

func publishableGoReleaserAsset(path, artifactType string) bool {
	if artifactType == "Binary" || artifactType == "binary" {
		return false
	}

	for _, suffix := range []string{
		"checksums.txt", ".tar.gz", ".tgz", ".zip", ".tar.xz", ".tar.bz2",
		".deb", ".rpm", ".apk", ".sbom.json",
	} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

type releaseFileCollector struct {
	entries []FileEntry
	paths   map[string]struct{}
	names   map[string]string
}

func (c *releaseFileCollector) add(ctx releaseFilesCtx, path, source string) error { //nolint:varnamelen // collector receiver.
	if err := validateReleaseDistAsset(ctx, path); err != nil {
		return err
	}

	if c.paths == nil {
		c.paths = map[string]struct{}{}
		c.names = map[string]string{}
	}

	if _, ok := c.paths[path]; ok {
		return nil
	}

	name := filepath.Base(path)
	if existing, ok := c.names[name]; ok {
		return fmt.Errorf("duplicate release asset basename: %s (first: %s, second: %s): %w", name, existing, path, errs.ErrValidation)
	}

	c.paths[path] = struct{}{}
	c.names[name] = path
	c.entries = append(c.entries, FileEntry{Path: path, Name: name, Source: source})

	return nil
}

func discoverSingleChecksum(ctx releaseFilesCtx) (string, error) {
	var files []string

	if err := filepath.WalkDir(ctx.distDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}

		if strings.HasSuffix(filepath.Base(path), "checksums.txt") {
			files = append(files, path)
		}

		return nil
	}); err != nil {
		return "", fmt.Errorf("discover checksums under %s: %w", ctx.distDir, err)
	}

	sort.Strings(files)

	if len(files) != 1 {
		return "", fmt.Errorf("expected exactly one checksums file in %s, found %d: %w", ctx.distDir, len(files), errs.ErrValidation)
	}

	return files[0], nil
}

func discoverDistSBOMEntries(ctx releaseFilesCtx) ([]FileEntry, error) {
	artifacts, err := readGoReleaserArtifacts(ctx)
	if err != nil {
		return nil, err
	}

	paths := []string{}

	for _, artifact := range artifacts {
		if publishableGoReleaserAsset(artifact.Path, artifact.Type) && strings.HasSuffix(artifact.Path, ".sbom.json") {
			paths = append(paths, artifact.Path)
		}
	}

	return entriesFromPaths(ctx, "sbom", paths)
}

func entriesFromPaths(ctx releaseFilesCtx, source string, paths []string) ([]FileEntry, error) {
	entries := make([]FileEntry, 0, len(paths))
	for _, path := range paths {
		if err := validateReleaseDistAsset(ctx, path); err != nil {
			return nil, err
		}

		entries = append(entries, FileEntry{Path: path, Name: filepath.Base(path), Source: source})
	}

	return entries, nil
}

func readReleaseAssetsJSON(path string) ([]FileEntry, error) {
	body, err := cliio.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release assets JSON %s: %w", path, err)
	}

	var object FileAssets
	if err := json.Unmarshal(body, &object); err == nil && object.Assets != nil {
		return object.Assets, nil
	}

	var assets []FileEntry
	if err := json.Unmarshal(body, &assets); err != nil {
		return nil, fmt.Errorf("parse release assets JSON %s: %w: %w", path, err, errs.ErrInvalidConfig)
	}

	return assets, nil
}

func readReleaseFileManifest(path string) (*FileManifest, error) {
	if !regularReleaseFile(path) {
		return nil, fmt.Errorf("release file manifest is missing, not a regular file, or a symlink: %s: %w", path, errs.ErrMissingInput)
	}

	body, err := os.ReadFile(path) //nolint:gosec // operator-supplied release metadata path.
	if err != nil {
		return nil, fmt.Errorf("read release file manifest %s: %w", path, err)
	}

	var manifest FileManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse release file manifest %s: %w: %w", path, err, errs.ErrInvalidConfig)
	}

	return &manifest, nil
}

func validateReleaseFileManifest(ctx releaseFilesCtx, manifest *FileManifest) error {
	if manifest.Version != releaseFilesVersion {
		return fmt.Errorf("release file manifest version must be %d: %w", releaseFilesVersion, errs.ErrValidation)
	}

	assetPaths, err := manifestAssetPaths(manifest)
	if err != nil {
		return err
	}

	allSections := [][]FileEntry{manifest.Assets, manifest.Checksums, manifest.SBOMs, manifest.Evidence, manifest.Provenance}
	if err := validateManifestSections(ctx, allSections); err != nil {
		return err
	}

	for _, classified := range [][]FileEntry{manifest.Checksums, manifest.SBOMs, manifest.Evidence, manifest.Provenance} {
		for _, entry := range classified {
			if _, ok := assetPaths[entry.Path]; !ok {
				return fmt.Errorf("manifest classified file %s must also be present in assets: %w", entry.Path, errs.ErrValidation)
			}
		}
	}

	if len(manifest.Checksums) != 1 {
		return fmt.Errorf("release file manifest must contain exactly one checksum file: %w", errs.ErrValidation)
	}

	return nil
}

// manifestAssetPaths indexes the manifest assets by path, rejecting duplicate
// basenames that would collide when published as release assets.
func manifestAssetPaths(manifest *FileManifest) (map[string]struct{}, error) {
	assetPaths := map[string]struct{}{}
	assetNames := map[string]string{}

	for _, asset := range manifest.Assets {
		assetPaths[asset.Path] = struct{}{}
		if previous, ok := assetNames[asset.Name]; ok && previous != asset.Path {
			return nil, fmt.Errorf("duplicate release asset basename in manifest: %s: %w", asset.Name, errs.ErrValidation)
		}

		assetNames[asset.Name] = asset.Path
	}

	return assetPaths, nil
}

// validateManifestSections checks every entry in every section and refuses the
// internal release image ledger anywhere in the manifest.
func validateManifestSections(ctx releaseFilesCtx, sections [][]FileEntry) error {
	for _, section := range sections {
		for _, entry := range section {
			if err := validateManifestEntry(ctx, entry); err != nil {
				return err
			}

			if filepath.Clean(entry.Path) == filepath.Join(ctx.distDir, "release-images.json") {
				return fmt.Errorf("release image ledger must not be in the release file manifest: %s: %w", entry.Path, errs.ErrValidation)
			}
		}
	}

	return nil
}

func validateManifestEntry(ctx releaseFilesCtx, entry FileEntry) error {
	if entry.Path == "" || entry.Name == "" || entry.Source == "" {
		return fmt.Errorf("release file manifest entries require non-empty path, name, and source: %w", errs.ErrValidation)
	}

	if entry.Name != filepath.Base(entry.Path) {
		return fmt.Errorf("release file manifest entry name %q must match basename of %q: %w", entry.Name, entry.Path, errs.ErrValidation)
	}

	return validateReleaseDistAsset(ctx, entry.Path)
}

func validateReleaseDistAsset(ctx releaseFilesCtx, path string) error {
	if !pathsafe.Relative(path) {
		return fmt.Errorf("unsafe artifact path: %s: %w", path, errs.ErrValidation)
	}

	distPrefix := filepath.ToSlash(ctx.distDir) + "/"
	if !strings.HasPrefix(filepath.ToSlash(path), distPrefix) {
		return fmt.Errorf("artifact path must be under %s/: %s: %w", ctx.distDir, path, errs.ErrValidation)
	}

	if !regularReleaseFile(path) {
		return fmt.Errorf("artifact path is missing, not a regular file, or a symlink: %s: %w", path, errs.ErrMissingInput)
	}

	return nil
}

func regularReleaseFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}

func releaseFileManifestAvailable(path string) bool {
	return regularReleaseFile(path)
}

func manifestSection(manifest *FileManifest, section string) ([]FileEntry, error) {
	switch section {
	case releaseFilesSectionAssets:
		return manifest.Assets, nil
	case "checksums":
		return manifest.Checksums, nil
	case "sboms":
		return manifest.SBOMs, nil
	case "evidence":
		return manifest.Evidence, nil
	case "provenance":
		return manifest.Provenance, nil
	default:
		return nil, fmt.Errorf("release files section must be one of assets, checksums, sboms, evidence, provenance (got %q): %w", section, errs.ErrUsage)
	}
}

func entryPaths(entries []FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}

	return paths
}

func allowedChecksumSubjects(ctx releaseFilesCtx) ([]string, error) {
	if releaseFileManifestAvailable(ctx.manifestFile) {
		return allowedChecksumSubjectsFromManifest(ctx)
	}

	return allowedChecksumSubjectsFromArtifacts(ctx)
}

func allowedChecksumSubjectsFromManifest(ctx releaseFilesCtx) ([]string, error) {
	manifest, err := readReleaseFileManifest(ctx.manifestFile)
	if err != nil {
		return nil, err
	}

	if err := validateReleaseFileManifest(ctx, manifest); err != nil {
		return nil, err
	}

	excluded := map[string]struct{}{}

	for _, section := range [][]FileEntry{manifest.Checksums, manifest.Provenance, manifest.Evidence} {
		for _, entry := range section {
			excluded[entry.Path] = struct{}{}
		}
	}

	allowed := []string{}

	for _, asset := range manifest.Assets {
		if _, ok := excluded[asset.Path]; ok {
			continue
		}

		if checksumSubjectExcluded(asset.Path) {
			continue
		}

		allowed = append(allowed, filepath.Base(asset.Path))
	}

	return uniqueSorted(allowed), nil
}

// checksumSubjectExcluded reports whether a manifest asset is metadata
// (signature sidecar, checksums file, provenance) rather than a checksum
// subject.
func checksumSubjectExcluded(path string) bool {
	base := filepath.Base(path)

	return strings.HasSuffix(path, ".bundle") || strings.HasSuffix(path, ".sig") || strings.HasSuffix(base, "checksums.txt") || base == "slsa-provenance.intoto.json"
}

func allowedChecksumSubjectsFromArtifacts(ctx releaseFilesCtx) ([]string, error) {
	artifacts, err := readGoReleaserArtifacts(ctx)
	if err != nil {
		return nil, err
	}

	allowed := []string{}

	for _, artifact := range artifacts {
		if !publishableGoReleaserAsset(artifact.Path, artifact.Type) || strings.HasSuffix(artifact.Path, "checksums.txt") {
			continue
		}

		if err := validateReleaseDistAsset(ctx, artifact.Path); err != nil {
			return nil, err
		}

		allowed = append(allowed, filepath.Base(artifact.Path))
	}

	return uniqueSorted(allowed), nil
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}

	return out
}

func sortedKeysMatching(counts map[string]int, keep func(string, int) bool) []string {
	keys := []string{}

	for key, count := range counts {
		if keep(key, count) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	return keys
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	sort.Strings(values)
	out := values[:0]

	var previous string
	for i, value := range values {
		if i == 0 || value != previous {
			out = append(out, value)
		}

		previous = value
	}

	return out
}
