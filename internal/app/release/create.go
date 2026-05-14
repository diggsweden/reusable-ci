// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
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
	// ListASCFiles returns every *.asc file in the cwd (or empty when
	// none).
	ListASCFiles() []string
}

// CreateReleaseInput drives `release create`. Mirrors the env contract
// of scripts/release/providers/github.sh.
type CreateReleaseInput struct {
	Tag              string
	Repository       string
	ReleaseName      string // defaults to Tag
	Draft            bool
	MakeLatest       bool
	AttachArtifacts  string // CSV of glob patterns
	ReleaseNotesFile string // default release.DefaultReleaseNotesFile
	ArtifactName     string // default basename(Repository)
	ChecksumsFile    string // default "checksums.sha256"
	ReleaseDir       string // default release.DefaultReleaseArtifactsDir
}

// CreateRelease delegates to Provider.CreateRelease after assembling
// the asset list from globs / release-artifacts dir / SBOM zip /
// checksums + remaining .asc signatures, deduped by basename.
func CreateRelease(
	ctx context.Context,
	prov provider.Provider,
	fs fsOps,
	out io.Writer,
	in CreateReleaseInput,
) error {
	if in.Tag == "" {
		return fmt.Errorf("TAG_NAME is required: %w", errs.ErrUsage)
	}
	if in.Repository == "" {
		return fmt.Errorf("REPOSITORY is required: %w", errs.ErrUsage)
	}
	notesFile := cmp.Or(in.ReleaseNotesFile, release.DefaultReleaseNotesFile)
	releaseName := cmp.Or(in.ReleaseName, in.Tag)
	releaseDir := cmp.Or(in.ReleaseDir, release.DefaultReleaseArtifactsDir)
	checksums := cmp.Or(in.ChecksumsFile, release.ChecksumsFile)
	artifactName := cmp.Or(in.ArtifactName, path.Base(in.Repository))
	version := strings.TrimPrefix(in.Tag, "v")
	specNotesFile := ""
	if fs.FileNonEmpty(notesFile) {
		specNotesFile = notesFile
	}

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
	for _, p := range fs.FindReleaseArtifacts(releaseDir) {
		candidates = append(candidates, p)
		// Add the .asc when colocated.
		if fs.FileExists(release.SignaturePath(filepath.Base(p))) {
			candidates = append(candidates, release.SignaturePath(filepath.Base(p)))
		}
	}

	// 3. SBOM zip + signature.
	sbomZip := sbom.ZipName(artifactName, version)
	if fs.FileExists(sbomZip) {
		fmt.Fprintf(out, "Adding SBOM ZIP: %s\n", sbomZip)
		candidates = append(candidates, sbomZip, release.SignaturePath(sbomZip))
	} else {
		fmt.Fprintf(out, "⚠️  SBOM ZIP not found: %s\n", sbomZip)
	}

	// 4. Checksums + signature.
	if fs.FileNonEmpty(checksums) {
		candidates = append(candidates, checksums, release.SignaturePath(checksums))
	} else {
		fmt.Fprintf(out, "⚠️  No %s or file is empty - skipping\n", checksums)
	}

	// 5. Any remaining .asc signatures the bash sweeps up.
	candidates = append(candidates, fs.ListASCFiles()...)

	// Filter to only paths that actually exist on disk (the bash skips
	// missing files silently via `[[ -f $file ]]`).
	keep := candidates[:0]
	for _, p := range candidates {
		if fs.FileExists(p) {
			keep = append(keep, p)
		}
	}
	assets := release.CollectAssets(keep)

	spec := provider.ReleaseSpec{
		Tag:        in.Tag,
		Name:       releaseName,
		NotesFile:  specNotesFile,
		Draft:      in.Draft,
		Prerelease: release.IsPrereleaseTag(in.Tag),
		MakeLatest: in.MakeLatest,
		Assets:     assets,
	}
	fmt.Fprintf(out, "Creating release %s with %d asset(s)\n", in.Tag, len(assets))
	return prov.CreateRelease(ctx, in.Repository, spec)
}
