// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// SBOMZipInput drives `reusable-ci release sbom-zip`.
type SBOMZipInput struct {
	ProjectName   string
	Version       string
	WorkingDir    string // default: cwd; root for *-sbom.{spdx,cyclonedx}.json globs
	SBOMDir       string // default: ./sbom-artifacts; analyzed-container SBOMs are flattened (no path)
	SignArtifacts bool   // when true (and signer != nil) emit <zip>.asc
	AssemblyFile  string // when set, bundle exactly the staged SBOM inputs
}

// SBOMZipResult reports what CreateSBOMZip wrote.
type SBOMZipResult struct {
	ZipName    string // empty when no SBOMs were found
	EntryCount int
	Signed     bool
}

// CreateSBOMZip bundles every *-sbom.{spdx,cyclonedx}.json in the working
// directory plus every analyzed-container SBOM under SBOMDir into a single
// zip. Returns ZipName="" when nothing was found. signer is consulted only when in.SignArtifacts is
// set and signer is non-nil.
func CreateSBOMZip(ctx context.Context, signer Signer, in SBOMZipInput, out io.Writer) (*SBOMZipResult, error) {
	if err := checkSBOMZipSigner(signer, in.SignArtifacts); err != nil {
		return nil, err
	}

	if in.AssemblyFile != "" {
		return createSBOMZipFromAssembly(ctx, signer, in, out)
	}

	in, ver := withSBOMZipDefaults(in)

	wdMatches, containerMatches, err := discoverSBOMs(in.WorkingDir, in.SBOMDir, in.ProjectName)
	if err != nil {
		return nil, err
	}

	if len(wdMatches)+len(containerMatches) == 0 {
		_, _ = fmt.Fprintln(out, "No SBOMs found, skipping ZIP creation")

		return &SBOMZipResult{}, nil
	}

	zipName := sbom.ZipName(in.ProjectName, ver)

	_, _ = fmt.Fprintln(out, "Creating SBOM zip archive with all 3 layers")

	count, err := writeSBOMZip(zipName, in.WorkingDir, wdMatches, containerMatches, out)
	if err != nil {
		return nil, err
	}

	res := &SBOMZipResult{ZipName: zipName, EntryCount: count}

	if in.SignArtifacts {
		if err := signSBOMZip(ctx, signer, zipName, out); err != nil {
			return res, err
		}

		res.Signed = true
	}

	_, _ = fmt.Fprintf(out, "Created SBOM ZIP: %s\n", zipName)

	return res, nil
}

func createSBOMZipFromAssembly(ctx context.Context, signer Signer, in SBOMZipInput, out io.Writer) (*SBOMZipResult, error) {
	asm, err := readAssembly(in.AssemblyFile)
	if err != nil {
		return nil, err
	}

	if len(asm.SBOMs) == 0 {
		_, _ = fmt.Fprintln(out, "No SBOMs found in release assembly, skipping ZIP creation")

		return &SBOMZipResult{}, nil
	}

	zipName := asm.SBOMZipFile
	if zipName == "" {
		ver := in.Version
		if ver == "" {
			ver = "unknown"
		}

		zipName = sbom.ZipName(in.ProjectName, version.StripVPrefix(ver))
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(zipName), 0o755); mkdirErr != nil { //nolint:gosec,mnd // public release staging dir.
		return nil, fmt.Errorf("create SBOM ZIP dir for %q: %w", zipName, mkdirErr)
	}

	_, _ = fmt.Fprintln(out, "Creating SBOM zip archive from release assembly")

	count, err := writeAssemblySBOMZip(zipName, asm.SBOMs, out)
	if err != nil {
		return nil, err
	}

	res := &SBOMZipResult{ZipName: zipName, EntryCount: count}
	if in.SignArtifacts {
		if err := signSBOMZip(ctx, signer, zipName, out); err != nil {
			return res, err
		}

		res.Signed = true
	}

	_, _ = fmt.Fprintf(out, "Created SBOM ZIP: %s\n", zipName)

	return res, nil
}

// discoverSBOMs lists SBOMs in the working dir (keeps relative path) and
// in the container SBOM dir (flattened later when written into the zip).
// First return is working-dir matches; second is container matches.
//
// When projectName is set, only SBOMs named "<projectName>-*" are taken.
// The zip is published and signed under the project's name, so an
// unscoped glob would sweep any *-sbom.*.json another tool left in the
// working directory into the release. Matching on the basename prefix
// rather than splicing projectName into the glob keeps a name carrying
// glob metacharacters inert.
func discoverSBOMs(workingDir, sbomDir, projectName string) ([]string, []string, error) {
	wdMatches, err := globAll(workingDir, domainrelease.SBOMFilePatterns)
	if err != nil {
		return nil, nil, err
	}

	containerMatches, err := globAll(sbomDir, []string{domainrelease.AnalyzedContainerSBOMPattern})
	if err != nil {
		return nil, nil, err
	}

	return scopeToProject(wdMatches, projectName), scopeToProject(containerMatches, projectName), nil
}

// scopeToProject keeps the matches whose basename belongs to projectName.
// An empty projectName keeps every match (the caller asked for no scope).
func scopeToProject(matches []string, projectName string) []string {
	if projectName == "" {
		return matches
	}

	kept := matches[:0]

	for _, match := range matches {
		if strings.HasPrefix(filepath.Base(match), projectName+"-") {
			kept = append(kept, match)
		}
	}

	return kept
}

// writeSBOMZip creates zipName and copies wdMatches (path relative to
// workingDir) + containerMatches (basename only) into it. Closes the
// zip writer and the underlying file on every path.
func writeSBOMZip(zipName, workingDir string, wdMatches, containerMatches []string, out io.Writer) (int, error) {
	zf, err := os.Create(zipName) //nolint:gosec // zipName is project/version-derived under cwd.
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", zipName, err)
	}

	w := zip.NewWriter(zf) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	defer func() { _ = zf.Close() }()
	defer func() { _ = w.Close() }()

	count := 0

	for _, m := range wdMatches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		rel, err := filepath.Rel(workingDir, m)
		if err != nil {
			rel = filepath.Base(m)
		}

		if err := addToZip(w, m, rel); err != nil {
			return 0, err
		}

		_, _ = fmt.Fprintf(out, "  Added: %s\n", rel)

		count++
	}

	for _, m := range containerMatches {
		base := filepath.Base(m)
		if err := addToZip(w, m, base); err != nil {
			return 0, err
		}

		_, _ = fmt.Fprintf(out, "  Added: %s\n", base)

		count++
	}

	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("close zip: %w", err)
	}

	if err := zf.Close(); err != nil {
		return 0, fmt.Errorf("close zip file: %w", err)
	}

	return count, nil
}

func writeAssemblySBOMZip(zipName string, entries []domainrelease.AssemblyFile, out io.Writer) (int, error) {
	zf, err := os.Create(zipName) //nolint:gosec // zipName is assembly-derived under release-files.
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", zipName, err)
	}

	w := zip.NewWriter(zf) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	defer func() { _ = zf.Close() }()
	defer func() { _ = w.Close() }()

	count := 0

	for _, entry := range entries {
		if !regularFileExists(entry.Path) {
			return 0, fmt.Errorf("assembly SBOM input %q is missing or not a regular file: %w", entry.Path, errs.ErrMissingInput)
		}

		name := entry.Name
		if name == "" {
			name = filepath.Base(entry.Path)
		}

		if err := addToZip(w, entry.Path, name); err != nil {
			return 0, err
		}

		_, _ = fmt.Fprintf(out, "  Added: %s\n", name)

		count++
	}

	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("close zip: %w", err)
	}

	if err := zf.Close(); err != nil {
		return 0, fmt.Errorf("close zip file: %w", err)
	}

	return count, nil
}

// withSBOMZipDefaults fills the discovery directories and returns the version
// used in the ZIP name, without a v prefix.
func withSBOMZipDefaults(in SBOMZipInput) (SBOMZipInput, string) {
	if in.WorkingDir == "" {
		in.WorkingDir = "."
	}

	if in.SBOMDir == "" {
		in.SBOMDir = domainrelease.DefaultSBOMArtifactsDir
	}

	ver := in.Version
	if ver == "" {
		ver = "unknown"
	}

	return in, version.StripVPrefix(ver)
}

// checkSBOMZipSigner checks a signing request before any SBOM is read or the
// ZIP written, so a missing or misconfigured signer leaves no unsigned ZIP
// behind.
func checkSBOMZipSigner(signer Signer, sign bool) error {
	if !sign {
		return nil
	}

	if signer == nil {
		return fmt.Errorf("sbom-zip: signing requested but no signer is configured: %w", errs.ErrUsage)
	}

	_, err := signerExtensions(signer)

	return err
}

func signSBOMZip(ctx context.Context, signer Signer, zipName string, out io.Writer) error {
	if err := signFileWithSidecars(ctx, signer, zipName); err != nil {
		return fmt.Errorf("sign sbom zip: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Signed SBOM ZIP: %s\n", zipName)

	return nil
}

func globAll(dir string, patterns []string) ([]string, error) {
	root, err := pathsafe.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	var out []string

	for _, p := range patterns {
		matches, err := fs.Glob(root.FS(), p)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", p, err)
		}

		for _, name := range matches {
			path := filepath.Join(dir, filepath.FromSlash(name))

			info, err := root.Lstat(filepath.FromSlash(name))
			if err != nil {
				return nil, err
			}

			if info.IsDir() {
				continue
			}

			if err := validateReleasePathComponents(path); err != nil {
				return nil, err
			}

			out = append(out, path)
		}
	}

	return out, nil
}

func addToZip(w *zip.Writer, srcPath, archivePath string) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	file, err := openReleaseFile(srcPath)
	if err != nil {
		return fmt.Errorf("open %q: %w", srcPath, err)
	}

	defer func() { _ = file.Close() }()

	zw, err := w.Create(archivePath)
	if err != nil {
		return fmt.Errorf("zip create %q: %w", archivePath, err)
	}

	if _, err := io.Copy(zw, file); err != nil {
		return fmt.Errorf("zip copy %q: %w", srcPath, err)
	}

	return nil
}
