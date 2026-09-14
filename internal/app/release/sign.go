// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
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
)

// Signer is the slice of the signing adapter the sign flow needs.
// internal/adapters/openpgp.Signer and the cosign-backed signer in
// signer_cosign.go both satisfy it in production; tests pass in-memory
// fakes.
//
// SignFile produces one sidecar file next to <file>; Extensions()
// lists the file extension(s) the backend writes (GPG: `.asc`;
// cosign: `.bundle` — a single self-contained v3 Sigstore bundle).
// The caller uses Extensions() to drive the rename logic when the
// input path is outside cwd, so per-method sidecar layouts compose
// with the same asset-walking loop.
//
// The interface intentionally hides the key material — backends are
// constructed once at process start, not per-file. Compare with the
// previous adapter that exposed (keyID, file) and required gpg-agent
// to hold the passphrase across multiple invocations.
type Signer interface {
	SignFile(ctx context.Context, file string) error
	Extensions() []string
}

// SignInput drives `reusable-ci release sign`.
type SignInput struct {
	ChecksumsFile           string // default: domainrelease.ChecksumsFile
	SkipChecksumsFile       bool
	ReleaseArtifactsDir     string // default: ./release-artifacts; signatures land in cwd as <basename>.asc
	SkipReleaseArtifactsDir bool
	AttachArtifacts         string // comma-separated glob list; signatures land in cwd as <basename>.asc
	Files                   []string
	AssemblyFile            string // when set, sign exactly the staged release assembly
	ReleaseFilesManifest    string // release-file manifest used by ManifestSections / ChecksumsFromManifest
	ReleaseFilesDistDir     string // dist directory used by ReleaseFilesManifest
	ManifestSections        []string
	ChecksumsFromManifest   bool
}

// SignArtifacts signs the checksums file in place (if present), each release
// artifact in ReleaseArtifactsDir, and each AttachArtifacts match. Asset
// signatures land in the working directory under <basename>.asc. The CLI wires
// openpgp.NewSignerFromArmor (env-sourced key); tests pass an in-memory fake.
//
//nolint:cyclop // a sequence of independent "sign this class if present" guards (checksums, release dir, attach globs).
func SignArtifacts(ctx context.Context, signer Signer, out io.Writer, in SignInput) error {
	if signer == nil {
		return fmt.Errorf("sign: signer is required: %w", errs.ErrUsage)
	}

	if _, err := signerExtensions(signer); err != nil {
		return err
	}

	if in.AssemblyFile != "" {
		return signAssemblyArtifacts(ctx, signer, out, in.AssemblyFile)
	}

	resolved, err := resolveSignInput(in)
	if err != nil {
		return err
	}

	in = resolved

	sectionFiles, err := releaseFilesManifestSectionFiles(in)
	if err != nil {
		return err
	}

	in.Files = append(in.Files, sectionFiles...)

	selected, err := selectExactSignFiles(in.Files)
	if err != nil {
		return err
	}

	var (
		assets          []string
		discoveryOutput bytes.Buffer
	)

	collect := func(path string) error {
		if err := validateReleasePathComponents(path); err != nil {
			return err
		}

		assets = append(assets, path)

		return nil
	}
	if !in.SkipReleaseArtifactsDir {
		if err := signReleaseArtifactsDir(in.ReleaseArtifactsDir, collect, &discoveryOutput); err != nil {
			return err
		}
	}

	if err := signAttachArtifacts(in.AttachArtifacts, &discoveryOutput, collect); err != nil {
		return err
	}

	if !in.SkipChecksumsFile {
		if err := validateReleasePathComponents(in.ChecksumsFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	_, _ = io.Copy(out, &discoveryOutput)
	if !in.SkipChecksumsFile {
		if checksumErr := signChecksumsIfPresent(ctx, signer, in.ChecksumsFile, out); checksumErr != nil {
			return checksumErr
		}
	}

	for _, file := range selected {
		_, _ = fmt.Fprintf(out, "Signing %s\n", file)
		if err := signFileWithSidecars(ctx, signer, file); err != nil {
			return fmt.Errorf("sign %q: %w", file, err)
		}
	}

	signAsset := newAssetSigner(ctx, signer, out)
	for _, path := range assets {
		if err := signAsset(path); err != nil {
			return err
		}
	}

	return nil
}

// resolveSignInput applies the checksums-file and release-artifacts-dir
// defaults and resolves --checksums-from-manifest before any signing starts.
func resolveSignInput(in SignInput) (SignInput, error) {
	if in.ChecksumsFile == "" && !in.SkipChecksumsFile {
		in.ChecksumsFile = domainrelease.ChecksumsFile
	}

	if in.ChecksumsFromManifest {
		checksumFile, err := manifestChecksumsFile(in)
		if err != nil {
			return in, err
		}

		in.ChecksumsFile = checksumFile
	}

	if in.ReleaseArtifactsDir == "" && !in.SkipReleaseArtifactsDir {
		in.ReleaseArtifactsDir = domainrelease.DefaultReleaseArtifactsDir
	}

	return in, nil
}

// manifestChecksumsFile validates the --checksums-from-manifest flag
// combination and returns the manifest's single validated checksums file.
func manifestChecksumsFile(in SignInput) (string, error) {
	if in.SkipChecksumsFile {
		return "", fmt.Errorf("sign: --checksums-from-manifest conflicts with skipping checksums: %w", errs.ErrInvalidConfig)
	}

	if in.ChecksumsFile != "" && in.ChecksumsFile != domainrelease.ChecksumsFile {
		return "", fmt.Errorf("sign: --checksums-from-manifest conflicts with explicit checksums file %q: %w", in.ChecksumsFile, errs.ErrInvalidConfig)
	}

	checksumFile, err := FindReleaseChecksumFile(releaseFilesSignInput(in))
	if err != nil {
		return "", err
	}

	if err := ValidateReleaseChecksums(ValidateReleaseChecksumsInput{FilesInput: releaseFilesSignInput(in), ChecksumsFile: checksumFile}); err != nil {
		return "", err
	}

	return checksumFile, nil
}

func releaseFilesSignInput(in SignInput) FilesInput {
	return FilesInput{DistDir: in.ReleaseFilesDistDir, ManifestFile: in.ReleaseFilesManifest}
}

func releaseFilesManifestSectionFiles(in SignInput) ([]string, error) {
	var files []string

	for _, section := range in.ManifestSections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		paths, err := ListReleaseFiles(ListReleaseFilesInput{FilesInput: releaseFilesSignInput(in), Section: section})
		if err != nil {
			return nil, err
		}

		files = append(files, paths...)
	}

	return files, nil
}

//nolint:cyclop // iterates the assembly's signable entries, dispatching each to the signer by method — a flat loop, not nested logic.
func signAssemblyArtifacts(ctx context.Context, signer Signer, out io.Writer, assemblyFile string) error {
	asm, err := readAssembly(assemblyFile)
	if err != nil {
		return err
	}

	selected := make([]string, 0, len(asm.Assets)+2)
	seen := map[string]struct{}{}
	selectFile := func(path string) error {
		if path == "" {
			return nil
		}

		if _, ok := seen[path]; ok {
			return nil
		}

		if !regularFileExists(path) {
			return fmt.Errorf("assembly sign target %q is missing or not a regular file: %w", path, errs.ErrMissingInput)
		}

		selected = append(selected, path)
		seen[path] = struct{}{}

		return nil
	}

	for _, asset := range asm.Assets {
		if err := selectFile(asset.Path); err != nil {
			return err
		}
	}

	if asm.SBOMZipFile != "" && regularFileExists(asm.SBOMZipFile) {
		if err := selectFile(asm.SBOMZipFile); err != nil {
			return err
		}
	}

	if asm.ChecksumFile != "" {
		if regularFileNonEmpty(asm.ChecksumFile) {
			if err := selectFile(asm.ChecksumFile); err != nil {
				return err
			}
		} else if len(asm.Assets) > 0 || regularFileExists(asm.SBOMZipFile) {
			return fmt.Errorf("assembly checksums file %q is missing or empty; run release checksums --assembly first: %w", asm.ChecksumFile, errs.ErrMissingInput)
		}
	}

	for _, path := range selected {
		_, _ = fmt.Fprintf(out, "Signing %s\n", path)
		if err := signFileWithSidecars(ctx, signer, path); err != nil {
			return fmt.Errorf("sign %q: %w", path, err)
		}
	}

	return nil
}

// selectExactSignFiles preflights operator-supplied files for in-place signing. Unlike
// AttachArtifacts, it does not glob and does not move sidecars to the current
// directory; callers use this for pre-validated release files whose sidecars
// must stay adjacent to the input file.
func selectExactSignFiles(files []string) ([]string, error) {
	selected := make([]string, 0, len(files))
	seen := map[string]struct{}{}

	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}

		if _, ok := seen[file]; ok {
			continue
		}

		if err := validateReleasePathComponents(file); err != nil {
			return nil, err
		}

		selected = append(selected, file)
		seen[file] = struct{}{}
	}

	return selected, nil
}

// signChecksumsIfPresent signs the checksums file when it exists and
// is non-empty. Missing or empty files are silently skipped — the
// release flow upstream decides whether to create one.
func signChecksumsIfPresent(ctx context.Context, signer Signer, checksumsFile string, out io.Writer) error {
	info, err := os.Stat(checksumsFile)
	if err != nil || info.Size() == 0 {
		return nil //nolint:nilerr // missing checksums is a valid state.
	}

	_, _ = fmt.Fprintf(out, "Signing %s\n", checksumsFile)

	if err := signFileWithSidecars(ctx, signer, checksumsFile); err != nil {
		return fmt.Errorf("sign checksums: %w", err)
	}

	return nil
}

// newAssetSigner returns a closure that signs a single artifact and
// moves each sidecar file (one or more per method — see
// Signer.Extensions) next to it (renaming when the source path
// differs from the cwd-relative target). The closure dedupes by
// basename so a file matched by both ReleaseArtifactsDir and
// AttachArtifacts is only signed once.
func newAssetSigner(ctx context.Context, signer Signer, out io.Writer) func(string) error {
	signed := map[string]struct{}{}

	return func(path string) error {
		base := filepath.Base(path)
		if _, ok := signed[base]; ok {
			return nil
		}

		_, _ = fmt.Fprintf(out, "Signing %s\n", base)

		if err := signFileWithSidecars(ctx, signer, path); err != nil {
			return fmt.Errorf("sign %q: %w", base, err)
		}

		for _, ext := range signer.Extensions() {
			oldSidecar := path + ext

			newSidecar := base + ext
			if oldSidecar == newSidecar {
				continue
			}

			if err := os.Rename(oldSidecar, newSidecar); err != nil {
				return fmt.Errorf("mv %q -> %q: %w", oldSidecar, newSidecar, err)
			}
		}

		signed[base] = struct{}{}

		return nil
	}
}

func signerExtensions(signer Signer) ([]string, error) {
	extensions := signer.Extensions()
	if len(extensions) == 0 {
		return nil, fmt.Errorf("signer advertises no signature sidecar extension: %w", errs.ErrInvalidConfig)
	}

	seen := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		if ext == "." || !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, "/\\\r\n") {
			return nil, fmt.Errorf("signer advertises unsafe signature sidecar extension %q: %w", ext, errs.ErrInvalidConfig)
		}

		if _, ok := seen[ext]; ok {
			return nil, fmt.Errorf("signer advertises duplicate signature sidecar extension %q: %w", ext, errs.ErrInvalidConfig)
		}

		seen[ext] = struct{}{}
	}

	return extensions, nil
}

// signFileWithSidecars removes stale outputs, invokes the backend, and proves
// that every advertised sidecar was freshly produced as a non-empty regular
// file before the release flow may continue.
//
//nolint:cyclop // preflight, stale-output removal and each produced sidecar fail independently.
func signFileWithSidecars(ctx context.Context, signer Signer, file string) error {
	if err := validateReleasePathComponents(file); err != nil {
		return err
	}

	extensions, err := signerExtensions(signer)
	if err != nil {
		return err
	}

	for _, ext := range extensions {
		if err := os.Remove(file + ext); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove stale sidecar %q: %w", file+ext, err)
		}
	}

	if err := signer.SignFile(ctx, file); err != nil {
		return err
	}

	for _, ext := range extensions {
		info, err := os.Lstat(file + ext)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("signer did not produce non-empty regular sidecar %q: %w", file+ext, errs.ErrValidation)
		}
	}

	return nil
}

// signReleaseArtifactsDir signs every regular file in dir that
// IsReleaseArtifact accepts. A missing directory is a valid state
// (attach globs may still match) and is silently skipped.
//
//nolint:cyclop // stat + branch on dir state + walk + count signed/skipped — phases of one operation.
func signReleaseArtifactsDir(dir string, signAsset func(string) error, out io.Writer) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("stat release artifacts dir %s: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s: not a directory: %w", dir, errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(out, "Signing individual artifacts")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %q: %w", dir, err)
	}

	signedCount := 0
	skippedCount := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		full := filepath.Join(dir, e.Name())
		if !domainrelease.IsReleaseArtifact(full) {
			skippedCount++

			continue
		}

		if err := signAsset(full); err != nil {
			return err
		}

		signedCount++
	}

	// Tell the user what happened when the filter rejected every file:
	// without this, a typo in --release-artifacts-dir or an unexpected
	// extension set (`.exe`, plain binaries) silently produced zero
	// signatures alongside an exit-0 "success" line.
	if signedCount == 0 && skippedCount > 0 {
		_, _ = fmt.Fprintf(out,
			"  no files in %q matched the release-artifact extension filter (%s); "+
				"use --attach-artifacts for bare binaries or other extensions\n",
			dir, strings.Join(domainrelease.ReleaseArtifactExtensions, ", "))
	}

	return nil
}

// signAttachArtifacts iterates each glob in patterns (comma-separated)
// and signs every match that exists and is a regular file. No-op when
// patterns is empty.
func signAttachArtifacts(patterns string, out io.Writer, sign func(path string) error) error {
	if patterns == "" {
		return nil
	}

	_, _ = fmt.Fprintf(out, "Signing attached artifacts matching: %s\n", patterns)

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

			if signErr := sign(m); signErr != nil {
				return signErr
			}
		}
	}

	return nil
}
