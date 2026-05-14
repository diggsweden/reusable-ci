// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// ChecksumsInput drives `reusable-ci release checksums`.
type ChecksumsInput struct {
	OutputFile          string // default: domain.ChecksumsFile
	ReleaseArtifactsDir string // default: ./release-artifacts
	AttachArtifacts     string // comma-separated glob list (verbatim from artifacts.yml)
	SBOMDir             string // default: ./sbom-artifacts
	WorkingDir          string // glob root for the *-sbom.{spdx,cyclonedx}.json patterns; default: cwd
}

// Checksums computes SHA256 over a fileset and writes lines of the form
//
//	<hex-hash>  <filename>
//
// to OutputFile (sha256sum-compatible). Mirrors
// scripts/release/generate-checksums.sh:
//
//   - release-artifacts/*       → basename in the manifest
//   - $ATTACH_ARTIFACTS globs   → original path in the manifest
//   - sbom-artifacts/*-analyzed-container-sbom.*.json → basename
//   - cwd *-sbom.{spdx,cyclonedx}.json → basename
//
// Returns the line count and any I/O error.
func Checksums(in ChecksumsInput, out io.Writer) (int, error) {
	if in.OutputFile == "" {
		in.OutputFile = domain.ChecksumsFile
	}
	if in.ReleaseArtifactsDir == "" {
		in.ReleaseArtifactsDir = domain.DefaultReleaseArtifactsDir
	}
	if in.SBOMDir == "" {
		in.SBOMDir = domain.DefaultSBOMArtifactsDir
	}

	f, err := os.Create(in.OutputFile)
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", in.OutputFile, err)
	}
	defer func() { _ = f.Close() }()

	count := 0
	write := func(path, label string) error {
		hash, err := sha256File(path)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s  %s\n", hash, label); err != nil {
			return err
		}
		count++
		return nil
	}

	// Release artefacts: basename labels.
	if entries, err := os.ReadDir(in.ReleaseArtifactsDir); err == nil {
		fmt.Fprintf(out, "→ Checksumming release artifacts from %s\n", in.ReleaseArtifactsDir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.Join(in.ReleaseArtifactsDir, e.Name())
			if err := write(full, e.Name()); err != nil {
				return count, err
			}
		}
	}

	// Attach-artifacts globs: keep original path in manifest.
	if in.AttachArtifacts != "" {
		fmt.Fprintf(out, "→ Checksumming attached artifacts matching: %s\n", in.AttachArtifacts)
		for _, raw := range strings.Split(in.AttachArtifacts, ",") {
			pattern := strings.TrimSpace(raw)
			if pattern == "" {
				continue
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return count, fmt.Errorf("glob %q: %w", pattern, err)
			}
			for _, m := range matches {
				if info, err := os.Stat(m); err == nil && !info.IsDir() {
					if err := write(m, m); err != nil {
						return count, err
					}
				}
			}
		}
	}

	// Container SBOMs: basename labels.
	if entries, err := os.ReadDir(in.SBOMDir); err == nil {
		fmt.Fprintf(out, "→ Checksumming container SBOMs from %s\n", in.SBOMDir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			matched, err := filepath.Match(domain.AnalyzedContainerSBOMPattern, e.Name())
			if err != nil || !matched {
				continue
			}
			full := filepath.Join(in.SBOMDir, e.Name())
			if err := write(full, e.Name()); err != nil {
				return count, err
			}
		}
	}

	// Working-dir SBOMs: basename labels.
	root := in.WorkingDir
	if root == "" {
		root = "."
	}
	fmt.Fprintln(out, "→ Checksumming all SBOM layers")
	for _, pattern := range domain.SBOMFilePatterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return count, fmt.Errorf("glob %q: %w", pattern, err)
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			if err := write(m, filepath.Base(m)); err != nil {
				return count, err
			}
		}
	}

	fmt.Fprintf(out, "✓ Generated %d checksums in %s\n", count, in.OutputFile)
	return count, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
