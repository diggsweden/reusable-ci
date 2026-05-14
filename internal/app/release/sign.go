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

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// gpgSigner is the slice of adapter/gpg.Adapter the sign flow needs.
// Defining a small interface keeps app-layer tests fake-able without
// importing the adapter or shelling to a real gpg binary.
type gpgSigner interface {
	DetachSign(ctx context.Context, keyID, file string) error
}

// SignInput drives `reusable-ci release sign`.
type SignInput struct {
	GPGKeyID            string // required
	ChecksumsFile       string // default: domain.ChecksumsFile
	ReleaseArtifactsDir string // default: ./release-artifacts; signatures land in cwd as <basename>.asc
}

// SignArtifacts signs the checksums file in place (if present) and signs
// each release artefact in ReleaseArtifactsDir, moving the resulting .asc
// to the working directory under the basename. The CLI wires gpg as
// adapter/gpg.New(); tests pass an in-memory fake.
//
// Mirrors scripts/release/sign-release-artifacts.sh.
func SignArtifacts(ctx context.Context, gpg gpgSigner, in SignInput, out io.Writer) error {
	if gpg == nil {
		return fmt.Errorf("sign: gpg signer is required: %w", errs.ErrUsage)
	}
	if in.GPGKeyID == "" {
		return fmt.Errorf("sign: GPG_KEY_ID is required: %w", errs.ErrUsage)
	}
	if in.ChecksumsFile == "" {
		in.ChecksumsFile = domain.ChecksumsFile
	}
	if in.ReleaseArtifactsDir == "" {
		in.ReleaseArtifactsDir = domain.DefaultReleaseArtifactsDir
	}

	if info, err := os.Stat(in.ChecksumsFile); err == nil && info.Size() > 0 {
		fmt.Fprintf(out, "Signing %s with GPG\n", in.ChecksumsFile)
		if err := gpg.DetachSign(ctx, in.GPGKeyID, in.ChecksumsFile); err != nil {
			return fmt.Errorf("sign checksums: %w", err)
		}
	}

	info, statErr := os.Stat(in.ReleaseArtifactsDir)
	switch {
	case errors.Is(statErr, fs.ErrNotExist):
		// No release-artifacts directory — nothing more to sign.
		return nil
	case statErr != nil:
		return fmt.Errorf("stat release artifacts dir %s: %w", in.ReleaseArtifactsDir, statErr)
	case !info.IsDir():
		return fmt.Errorf("%s: not a directory", in.ReleaseArtifactsDir)
	}

	fmt.Fprintln(out, "Signing individual artifacts")
	entries, err := os.ReadDir(in.ReleaseArtifactsDir)
	if err != nil {
		return fmt.Errorf("read %q: %w", in.ReleaseArtifactsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(in.ReleaseArtifactsDir, e.Name())
		if !domain.IsReleaseArtifact(full) {
			continue
		}
		fmt.Fprintf(out, "Signing %s\n", e.Name())
		if err := gpg.DetachSign(ctx, in.GPGKeyID, full); err != nil {
			return fmt.Errorf("sign %q: %w", e.Name(), err)
		}
		// `gpg --detach-sign` produces <full>.asc next to the artefact;
		// move it to cwd under the basename for the GitHub Release upload.
		oldAsc := full + ".asc"
		newAsc := e.Name() + ".asc"
		if err := os.Rename(oldAsc, newAsc); err != nil {
			return fmt.Errorf("mv %q -> %q: %w", oldAsc, newAsc, err)
		}
	}
	return nil
}
