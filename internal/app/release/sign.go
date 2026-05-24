// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
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
	ChecksumsFile       string // default: domain.ChecksumsFile
	ReleaseArtifactsDir string // default: ./release-artifacts; signatures land in cwd as <basename>.asc
	AttachArtifacts     string // comma-separated glob list; signatures land in cwd as <basename>.asc
}

// SignArtifacts signs the checksums file in place (if present), each release
// artefact in ReleaseArtifactsDir, and each AttachArtifacts match. Asset
// signatures land in the working directory under <basename>.asc. The CLI wires
// openpgp.NewSignerFromArmor (env-sourced key); tests pass an in-memory fake.
func SignArtifacts(ctx context.Context, signer Signer, out io.Writer, in SignInput) error {
	if signer == nil {
		return fmt.Errorf("sign: signer is required: %w", errs.ErrUsage)
	}

	if in.ChecksumsFile == "" {
		in.ChecksumsFile = domain.ChecksumsFile
	}

	if in.ReleaseArtifactsDir == "" {
		in.ReleaseArtifactsDir = domain.DefaultReleaseArtifactsDir
	}

	if err := signChecksumsIfPresent(ctx, signer, in.ChecksumsFile, out); err != nil {
		return err
	}

	signAsset := newAssetSigner(ctx, signer, out)

	if err := signReleaseArtifactsDir(in.ReleaseArtifactsDir, signAsset, out); err != nil {
		return err
	}

	return signAttachArtifacts(in.AttachArtifacts, out, signAsset)
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

	if err := signer.SignFile(ctx, checksumsFile); err != nil {
		return fmt.Errorf("sign checksums: %w", err)
	}

	return nil
}

// newAssetSigner returns a closure that signs a single artefact and
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

		if err := signer.SignFile(ctx, path); err != nil {
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

// signReleaseArtifactsDir signs every regular file in dir that
// IsReleaseArtifact accepts. A missing directory is a valid state
// (attach globs may still match) and is silently skipped.
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
		if !domain.IsReleaseArtifact(full) {
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
			dir, strings.Join(domain.ReleaseArtifactExtensions, ", "))
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
