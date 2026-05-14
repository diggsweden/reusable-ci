// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
)

// SBOMZipInput drives `reusable-ci release sbom-zip`.
type SBOMZipInput struct {
	ProjectName   string
	Version       string
	WorkingDir    string // default: cwd; root for *-sbom.{spdx,cyclonedx}.json globs
	SBOMDir       string // default: ./sbom-artifacts; analyzed-container SBOMs are flattened (no path)
	SignArtifacts bool   // when true (and GPGKeyID set) emit <zip>.asc
	GPGKeyID      string // identifies the secret key for the signature
}

// SBOMZipResult reports what CreateSBOMZip wrote.
type SBOMZipResult struct {
	ZipName    string // empty when no SBOMs were found
	EntryCount int
	Signed     bool
}

// CreateSBOMZip bundles every *-sbom.{spdx,cyclonedx}.json in the working
// directory plus every analyzed-container SBOM under SBOMDir into a single
// zip. Returns ZipName="" when nothing was found (matches the bash no-op
// exit-0 behaviour). gpg is consulted only when in.SignArtifacts and
// in.GPGKeyID are both set; pass nil when signing is disabled.
//
// Mirrors scripts/release/create-sbom-zip.sh.
func CreateSBOMZip(ctx context.Context, gpg gpgSigner, in SBOMZipInput, out io.Writer) (*SBOMZipResult, error) {
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

	wdMatches, err := globAll(in.WorkingDir, domain.SBOMFilePatterns)
	if err != nil {
		return nil, err
	}
	containerMatches, _ := globAll(in.SBOMDir, []string{domain.AnalyzedContainerSBOMPattern})

	if len(wdMatches)+len(containerMatches) == 0 {
		fmt.Fprintln(out, "No SBOMs found, skipping ZIP creation")
		return &SBOMZipResult{}, nil
	}

	zipName := sbom.ZipName(in.ProjectName, ver)
	fmt.Fprintln(out, "Creating SBOM zip archive with all 3 layers")

	zf, err := os.Create(zipName)
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", zipName, err)
	}
	w := zip.NewWriter(zf)
	count := 0

	// Working-dir SBOMs: keep relative path inside the zip.
	for _, m := range wdMatches {
		rel, err := filepath.Rel(in.WorkingDir, m)
		if err != nil {
			rel = filepath.Base(m)
		}
		if err := addToZip(w, m, rel); err != nil {
			_ = w.Close()
			_ = zf.Close()
			return nil, err
		}
		fmt.Fprintf(out, "  Added: %s\n", rel)
		count++
	}
	// Container SBOMs: flatten path (zip -j equivalent).
	for _, m := range containerMatches {
		base := filepath.Base(m)
		if err := addToZip(w, m, base); err != nil {
			_ = w.Close()
			_ = zf.Close()
			return nil, err
		}
		fmt.Fprintf(out, "  Added: %s\n", base)
		count++
	}

	if err := w.Close(); err != nil {
		_ = zf.Close()
		return nil, fmt.Errorf("close zip: %w", err)
	}
	if err := zf.Close(); err != nil {
		return nil, fmt.Errorf("close zip file: %w", err)
	}

	res := &SBOMZipResult{ZipName: zipName, EntryCount: count}

	if in.SignArtifacts && in.GPGKeyID != "" {
		if gpg == nil {
			return res, errors.New("sbom-zip: sign requested but gpg signer is nil")
		}
		if err := gpg.DetachSign(ctx, in.GPGKeyID, zipName); err != nil {
			return res, fmt.Errorf("sign sbom zip: %w", err)
		}
		res.Signed = true
		fmt.Fprintf(out, "Signed SBOM ZIP: %s.asc\n", zipName)
	}

	fmt.Fprintf(out, "Created SBOM ZIP: %s\n", zipName)
	return res, nil
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

func addToZip(w *zip.Writer, srcPath, archivePath string) error {
	f, err := os.Open(srcPath)
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
