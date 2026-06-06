// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/cliio"
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
// to OutputFile (sha256sum-compatible). The fileset is composed from:
//
//   - release-artifacts/*       → basename in the manifest
//   - $ATTACH_ARTIFACTS globs   → original path in the manifest
//   - sbom-artifacts/*-analyzed-container-sbom.*.json → basename
//   - cwd *-sbom.{spdx,cyclonedx}.json → basename
//
// Returns the line count and any I/O error.
//nolint:cyclop // emits checksum file + uploads + summary entry per artifact.
func Checksums(out io.Writer, in ChecksumsInput) (int, error) {
	if in.OutputFile == "" {
		in.OutputFile = domain.ChecksumsFile
	}

	if in.ReleaseArtifactsDir == "" {
		in.ReleaseArtifactsDir = domain.DefaultReleaseArtifactsDir
	}

	if in.SBOMDir == "" {
		in.SBOMDir = domain.DefaultSBOMArtifactsDir
	}

	f, err := cliio.CreateWriter(in.OutputFile, 0o644) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	// Compute the output-file absolute path once so the per-walk
	// helpers can skip it. Without this guard, pointing
	// --release-artifacts-dir at the directory that holds the
	// checksums file itself would hash the freshly-created empty
	// file (sha256 e3b0c44…) and the resulting manifest would fail
	// `sha256sum --check` against itself.
	outputAbs := absOrSelf(in.OutputFile)

	skip := func(path string) bool {
		return outputAbs != "" && absOrSelf(path) == outputAbs
	}

	// Release artefacts: basename labels.
	if err := checksumReleaseArtifacts(in.ReleaseArtifactsDir, out, write, skip); err != nil {
		return count, err
	}

	// Attach-artifacts globs: keep original path in manifest.
	if err := checksumAttachArtifacts(in.AttachArtifacts, out, write, skip); err != nil {
		return count, err
	}

	// Container SBOMs: basename labels.
	if err := checksumContainerSBOMs(in.SBOMDir, out, write, skip); err != nil {
		return count, err
	}

	// Working-dir SBOMs: basename labels.
	if err := checksumWorkdirSBOMs(in.WorkingDir, out, write, skip); err != nil {
		return count, err
	}

	_, _ = fmt.Fprintf(out, "✓ Generated %d checksums in %s\n", count, in.OutputFile)

	return count, nil
}

// checksumReleaseArtifacts walks dir (one level) and invokes write for
// every regular file, labelling the manifest entry with basename. A
// missing dir is silently treated as empty. skip lets the caller
// exclude the output manifest itself.
func checksumReleaseArtifacts(dir string, out io.Writer, write func(absPath, label string) error, skip func(string) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil //nolint:nilerr // missing dir → no entries (matches bash behaviour)
	}

	_, _ = fmt.Fprintf(out, "→ Checksumming release artifacts from %s\n", dir)

	for _, e := range entries { //nolint:varnamelen // idiomatic loop var for os.DirEntry.
		if e.IsDir() {
			continue
		}

		path := filepath.Join(dir, e.Name())
		if skip != nil && skip(path) {
			continue
		}

		if err := write(path, e.Name()); err != nil {
			return err
		}
	}

	return nil
}

// checksumContainerSBOMs walks dir for files matching the
// analyzed-container SBOM pattern. Missing dir → no-op. skip excludes
// the output manifest when it lives inside the walked directory.
func checksumContainerSBOMs(dir string, out io.Writer, write func(absPath, label string) error, skip func(string) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil //nolint:nilerr // missing dir → no entries
	}

	_, _ = fmt.Fprintf(out, "→ Checksumming container SBOMs from %s\n", dir)

	for _, e := range entries { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if e.IsDir() {
			continue
		}

		matched, err := filepath.Match(domain.AnalyzedContainerSBOMPattern, e.Name())
		if err != nil || !matched {
			continue
		}

		path := filepath.Join(dir, e.Name())
		if skip != nil && skip(path) {
			continue
		}

		if err := write(path, e.Name()); err != nil {
			return err
		}
	}

	return nil
}

// checksumWorkdirSBOMs globs the SBOM-layer patterns rooted at workdir
// (defaulting to ".") and invokes write for each regular-file match.
// skip excludes the output manifest when it sits in workdir.
func checksumWorkdirSBOMs(workdir string, out io.Writer, write func(absPath, label string) error, skip func(string) bool) error {
	root := workdir
	if root == "" {
		root = "."
	}

	_, _ = fmt.Fprintln(out, "→ Checksumming all SBOM layers")

	for _, pattern := range domain.SBOMFilePatterns {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return fmt.Errorf("glob %q: %w", pattern, err)
		}

		for _, m := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			info, statErr := os.Stat(m)
			if statErr != nil || info.IsDir() {
				continue
			}

			if skip != nil && skip(m) {
				continue
			}

			if err := write(m, filepath.Base(m)); err != nil {
				return err
			}
		}
	}

	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec,varnamelen // checksumming caller-supplied artefact path.
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

// checksumAttachArtifacts iterates each glob in patterns (comma-separated)
// and invokes write(absPath, manifestLabel) for every match that exists
// and is a regular file. No-op when patterns is empty. skip excludes
// the output manifest when a glob accidentally matches it.
//nolint:cyclop // CSV → globs → stat → filter → write — one branch per phase.
func checksumAttachArtifacts(patterns string, out io.Writer, write func(absPath, label string) error, skip func(string) bool) error {
	if patterns == "" {
		return nil
	}

	_, _ = fmt.Fprintf(out, "→ Checksumming attached artifacts matching: %s\n", patterns)

	for _, raw := range strings.Split(patterns, ",") {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}

		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("glob %q: %w", pattern, err)
		}

		for _, m := range matches { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			info, statErr := os.Stat(m)
			if statErr != nil || info.IsDir() {
				continue
			}

			if skip != nil && skip(m) {
				continue
			}

			if writeErr := write(m, m); writeErr != nil {
				return writeErr
			}
		}
	}

	return nil
}

// absOrSelf returns filepath.Abs(path) when possible, otherwise the
// input unchanged. Used to compare an arbitrary input path against the
// manifest output path without erroring out when either is already
// absolute or filesystem-resolution fails.
func absOrSelf(path string) string {
	if path == "" {
		return ""
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}
