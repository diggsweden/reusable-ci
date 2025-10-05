// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// The rules on this file's inputs used to live in
// cli/commands/container/releaseimages.go -- registry-reference parsing and
// the ledger-path confinement, sitting in the driving layer beside flag
// plumbing. They are behaviour, not plumbing: the CLI layer is the least
// tested in the repository, and a simplicity review found exactly these
// functions carrying real decisions there. Moved verbatim; the CLI files
// keep only flag reading and credential resolution.

// ValidateReleaseImagesPath confines the ledger path under the dist
// directory: non-empty, strictly below distDir, and a safe relative suffix
// per pathsafe.Relative.
func ValidateReleaseImagesPath(path, distDir string) error {
	if path == "" {
		return fmt.Errorf("release images: ledger path is required: %w", errs.ErrUsage)
	}

	distDir = strings.TrimRight(distDir, "/")
	if distDir == "" {
		return fmt.Errorf("release images: dist dir is required: %w", errs.ErrUsage)
	}

	if !strings.HasPrefix(path, distDir+"/") {
		return fmt.Errorf("release images: ledger path must stay under %s/: %s: %w", distDir, path, errs.ErrUsage)
	}

	// The suffix under dist answers to the one workspace-relative rule in
	// internal/pathsafe rather than to a hand-rolled '..' loop. This also
	// refuses control characters and a doubled slash after the dist directory.
	if !pathsafe.Relative(strings.TrimPrefix(path, distDir+"/")) {
		return fmt.Errorf("release images: ledger path must be a safe relative path under %s/ without '..': %s: %w", distDir, path, errs.ErrUsage)
	}

	root, err := pathsafe.OpenRoot(filepath.Dir(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	info, err := root.Lstat(filepath.Base(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("release images: ledger must be a regular file: %w", errs.ErrValidation)
	}

	return nil
}

// DefaultReleaseImageRepository derives the registry repository for a
// release image from the forge host and the repository slug.
func DefaultReleaseImageRepository(serverHost, repository string) string {
	return serverHost + "/" + strings.ToLower(repository)
}

// NormalizedServerURL trims a server URL flag and defaults its scheme to
// https; a value that already carries a scheme is kept as written.
func NormalizedServerURL(raw string) string {
	raw = strings.TrimSpace(raw)

	// Trailing slashes are trimmed from what follows the scheme, never from the
	// scheme itself. Trimming the whole value turned "https://" into "https:",
	// which has no "://" and so gained a second scheme -- "https://https:" --
	// that the host parser accepted as the registry "https:" with no error.
	if scheme, rest, found := strings.Cut(raw, "://"); found {
		return scheme + "://" + strings.TrimRight(rest, "/")
	}

	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}

	return "https://" + raw
}

// OptionalRegistryHost is RegistryHost for a flag that may be unset.
func OptionalRegistryHost(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	return RegistryHost(raw)
}

// RegistryHost reduces a registry reference -- with or without scheme, with
// or without a repository path -- to its bare host.
func RegistryHost(raw string) (string, error) {
	host, err := domaincontainer.RegistryHost(raw)
	if err != nil {
		return "", fmt.Errorf("release images: %w", err)
	}

	return host, nil
}
