// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// fsOps is the slice of filesystem queries the use case needs. Tests
// substitute an in-memory implementation so the asset-collection logic
// can be exercised without touching the disk.
type fsOps interface {
	// FileExists reports whether path is a regular file (non-empty
	// existence check). Used as the gate for SBOM/checksum inclusion.
	FileExists(path string) bool
	// FileNonEmpty reports whether path exists AND has size > 0.
	FileNonEmpty(path string) bool
	// FindReleaseArtifacts walks dir and returns every file with a
	// recognised release extension (excluding original-*.jar).
	FindReleaseArtifacts(dir string) []string
	// Glob expands a shell-style pattern (matches filepath.Glob).
	Glob(pattern string) []string
	// ListSignatureSidecars returns every signature sidecar in the
	// cwd across all supported methods (*.asc for GPG, *.bundle for
	// cosign). Empty when none exist.
	ListSignatureSidecars() []string
}

// CreateReleaseInput drives `release create`. Mirrors the env contract.
type CreateReleaseInput struct {
	Tag              string
	Repository       string
	ReleaseName      string // defaults to Tag
	Draft            bool
	MakeLatest       string
	AttachArtifacts  string // CSV of glob patterns
	ReleaseNotesFile string // default release.DefaultReleaseNotesFile
	ArtifactName     string // default basename(Repository)
	ChecksumsFile    string // default "checksums.sha256"
	ReleaseDir       string // default release.DefaultReleaseArtifactsDir
	AssemblyFile     string // when set, upload exactly the staged release assembly
}

// CreateRelease delegates to the ReleaseCreator after assembling the
// asset list from globs / release-artifacts dir / SBOM zip / checksums
// + remaining .asc signatures, deduped by basename. The local provider
// does not satisfy ReleaseCreator — the CLI gates on platform before
// reaching this use case.
//
//nolint:cyclop // release flow: notes → assets → provider call → summary.
func CreateRelease(
	ctx context.Context,
	prov provider.ReleaseCreator,
	fs fsOps,
	out io.Writer,
	in CreateReleaseInput,
) error {
	if in.Tag == "" {
		return fmt.Errorf("tag is required: pass --tag <name> or set $TAG_NAME: %w", errs.ErrUsage)
	}

	if in.Repository == "" {
		return fmt.Errorf("repository is required: pass --repository <owner/repo> or set $REPOSITORY: %w", errs.ErrUsage)
	}

	notesFile := cmp.Or(in.ReleaseNotesFile, release.DefaultReleaseNotesFile)
	releaseName := cmp.Or(in.ReleaseName, in.Tag)
	releaseDir := cmp.Or(in.ReleaseDir, release.DefaultReleaseArtifactsDir)
	checksums := cmp.Or(in.ChecksumsFile, release.ChecksumsFile)
	artifactName := cmp.Or(in.ArtifactName, path.Base(in.Repository))
	version := domainversion.StripVPrefix(in.Tag)

	makeLatest, err := normalizeMakeLatest(in.MakeLatest)
	if err != nil {
		return err
	}

	specNotesFile := ""
	if fs.FileNonEmpty(notesFile) {
		specNotesFile = notesFile
	}

	assets, err := collectCreateAssets(fs, out, createAssetInput{
		AssemblyFile:    in.AssemblyFile,
		AttachArtifacts: in.AttachArtifacts,
		ReleaseDir:      releaseDir,
		ArtifactName:    artifactName,
		Version:         version,
		Checksums:       checksums,
	})
	if err != nil {
		return err
	}

	spec := provider.ReleaseSpec{
		Tag:        in.Tag,
		Name:       releaseName,
		NotesFile:  specNotesFile,
		Draft:      in.Draft,
		Prerelease: release.IsPrereleaseTag(in.Tag),
		MakeLatest: makeLatest,
		Assets:     assets,
	}
	_, _ = fmt.Fprintf(out, "Creating release %s with %d asset(s)\n", in.Tag, len(assets))

	return prov.CreateRelease(ctx, in.Repository, spec)
}

type createAssetInput struct {
	AssemblyFile    string
	AttachArtifacts string
	ReleaseDir      string
	ArtifactName    string
	Version         string
	Checksums       string
}

func collectCreateAssets(fs fsOps, out io.Writer, in createAssetInput) ([]string, error) {
	if in.AssemblyFile != "" {
		return collectCreateAssetsFromAssembly(in.AssemblyFile)
	}

	return collectCreateAssetsLegacy(fs, out, in), nil
}

//nolint:cyclop // a sequence of independent "add this asset class if present" guards (release dir, checksums, sigs, sbom zip, attachments).
func collectCreateAssetsLegacy(fs fsOps, out io.Writer, in createAssetInput) []string {
	candidates := []string{}

	// 1. Pattern-glob artifacts (CSV of globs).
	if in.AttachArtifacts != "" {
		for _, pat := range strings.Split(in.AttachArtifacts, ",") {
			pat = strings.TrimSpace(pat)
			if pat == "" {
				continue
			}

			matches := fs.Glob(pat)
			sort.Strings(matches) // stable order across runs
			candidates = append(candidates, matches...)
		}
	}

	// 2. Release-artifacts directory (recursive, recognised extensions).
	for _, p := range fs.FindReleaseArtifacts(in.ReleaseDir) {
		candidates = append(candidates, p)
		// Add every possible sidecar (.asc, .bundle) when colocated.
		// Only one will exist in practice — the producer chose one
		// method per repo via artifacts.yml's sign.method.
		for _, sidecar := range release.SignatureSidecars(filepath.Base(p)) {
			if fs.FileExists(sidecar) {
				candidates = append(candidates, sidecar)
			}
		}
	}

	// 3. SBOM zip + signature sidecars.
	sbomZip := sbom.ZipName(in.ArtifactName, in.Version)
	if fs.FileExists(sbomZip) {
		_, _ = fmt.Fprintf(out, "Adding SBOM ZIP: %s\n", sbomZip)
		candidates = append(candidates, sbomZip)
		candidates = append(candidates, release.SignatureSidecars(sbomZip)...)
	} else {
		_, _ = fmt.Fprintf(out, "⚠️  SBOM ZIP not found: %s\n", sbomZip)
	}

	// 4. Checksums + signature sidecars.
	if fs.FileNonEmpty(in.Checksums) {
		candidates = append(candidates, in.Checksums)
		candidates = append(candidates, release.SignatureSidecars(in.Checksums)...)
	} else {
		_, _ = fmt.Fprintf(out, "⚠️  No %s or file is empty - skipping\n", in.Checksums)
	}

	// 5. Any remaining signature sidecars the bash sweep picks up
	// (.asc for GPG, .bundle for cosign).
	candidates = append(candidates, fs.ListSignatureSidecars()...)

	// Filter to only paths that actually exist on disk (the bash skips
	// missing files silently via `[[ -f $file ]]`).
	keep := candidates[:0]
	for _, p := range candidates {
		if fs.FileExists(p) {
			keep = append(keep, p)
		}
	}

	return release.CollectAssets(keep)
}

//nolint:cyclop // walks the assembly manifest, classifying each entry into one asset bucket — a flat dispatch, not nested logic.
func collectCreateAssetsFromAssembly(path string) ([]string, error) {
	asm, err := readAssembly(path)
	if err != nil {
		return nil, err
	}

	assets := make([]string, 0, len(asm.Assets)+4)
	seen := map[string]string{}
	add := func(path string, required bool) error {
		if path == "" {
			return nil
		}

		if !regularFileExists(path) {
			if required {
				return fmt.Errorf("assembly release asset %q is missing or not a regular file: %w", path, errs.ErrMissingInput)
			}

			return nil
		}

		base := filepath.Base(path)
		if previous, ok := seen[base]; ok {
			if previous == path {
				return nil
			}

			return fmt.Errorf("release asset basename %q is duplicated by %s and %s: %w", base, previous, path, errs.ErrValidation)
		}

		seen[base] = path
		assets = append(assets, path)

		return nil
	}

	addWithSidecars := func(path string, required bool) error {
		if err := add(path, required); err != nil {
			return err
		}

		for _, sidecar := range release.SignatureSidecars(path) {
			if err := add(sidecar, false); err != nil {
				return err
			}
		}

		return nil
	}

	for _, asset := range asm.Assets {
		if err := addWithSidecars(asset.Path, true); err != nil {
			return nil, err
		}
	}

	if asm.SBOMZipFile != "" {
		if err := addWithSidecars(asm.SBOMZipFile, len(asm.SBOMs) > 0); err != nil {
			return nil, err
		}
	}

	if asm.ChecksumFile != "" && regularFileNonEmpty(asm.ChecksumFile) {
		if err := addWithSidecars(asm.ChecksumFile, true); err != nil {
			return nil, err
		}
	}

	return assets, nil
}

func normalizeMakeLatest(value string) (provider.MakeLatestMode, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return provider.MakeLatestTrue, nil
	}

	switch provider.MakeLatestMode(value) {
	case provider.MakeLatestTrue, provider.MakeLatestFalse, provider.MakeLatestLegacy:
		return provider.MakeLatestMode(value), nil
	default:
		return "", fmt.Errorf("make-latest must be one of true, false, legacy (got %q): %w", value, errs.ErrUsage)
	}
}
