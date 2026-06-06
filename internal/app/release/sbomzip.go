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

	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
	"github.com/diggsweden/reusable-ci/internal/domain/sbom"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// SBOMZipInput drives `reusable-ci release sbom-zip`.
type SBOMZipInput struct {
	ProjectName   string
	Version       string
	WorkingDir    string // default: cwd; root for *-sbom.{spdx,cyclonedx}.json globs
	SBOMDir       string // default: ./sbom-artifacts; analyzed-container SBOMs are flattened (no path)
	SignArtifacts bool   // when true (and signer != nil) emit <zip>.asc
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
	if in.WorkingDir == "" {
		in.WorkingDir = "."
	}

	if in.SBOMDir == "" {
		in.SBOMDir = domain.DefaultSBOMArtifactsDir
	}

	ver := in.Version
	if ver == "" {
		ver = "unknown"
	}

	ver = version.StripVPrefix(ver)

	wdMatches, containerMatches, err := discoverSBOMs(in.WorkingDir, in.SBOMDir)
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

// discoverSBOMs lists SBOMs in the working dir (keeps relative path) and
// in the container SBOM dir (flattened later when written into the zip).
// First return is working-dir matches; second is container matches.
func discoverSBOMs(workingDir, sbomDir string) ([]string, []string, error) {
	wdMatches, err := globAll(workingDir, domain.SBOMFilePatterns)
	if err != nil {
		return nil, nil, err
	}

	containerMatches, _ := globAll(sbomDir, []string{domain.AnalyzedContainerSBOMPattern})

	return wdMatches, containerMatches, nil
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

func signSBOMZip(ctx context.Context, signer Signer, zipName string, out io.Writer) error {
	if signer == nil {
		return fmt.Errorf("sbom-zip: sign requested but signer is nil: %w", errs.ErrValidation)
	}

	if err := signer.SignFile(ctx, zipName); err != nil {
		return fmt.Errorf("sign sbom zip: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Signed SBOM ZIP: %s.asc\n", zipName)

	return nil
}

func globAll(dir string, patterns []string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing directory → empty result, matching bash's silent
			// skip; only this specific error swallows.
			return nil, nil
		}

		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}

	var out []string

	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, p))
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", p, err)
		}

		for _, m := range matches {
			if info, err := os.Stat(m); err == nil && !info.IsDir() {
				out = append(out, m)
			}
		}
	}

	return out, nil
}

func addToZip(w *zip.Writer, srcPath, archivePath string) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	f, err := os.Open(srcPath) //nolint:gosec,varnamelen // srcPath is built from CLI inputs + globbed SBOM matches.
	if err != nil {
		return fmt.Errorf("open %q: %w", srcPath, err)
	}

	defer func() { _ = f.Close() }()

	zw, err := w.Create(archivePath)
	if err != nil {
		return fmt.Errorf("zip create %q: %w", archivePath, err)
	}

	if _, err := io.Copy(zw, f); err != nil {
		return fmt.Errorf("zip copy %q: %w", srcPath, err)
	}

	return nil
}
