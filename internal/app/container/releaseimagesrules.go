// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

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

	if path == distDir || !strings.HasPrefix(path, distDir+"/") {
		return fmt.Errorf("release images: ledger path must stay under %s/: %s: %w", distDir, path, errs.ErrUsage)
	}

	// The suffix under dist answers to the one workspace-relative rule in
	// internal/pathsafe rather than to a fourth hand-rolled '..' loop -- the
	// convergence docs/open-questions.md tracks. Two spellings this refuses
	// that the old loop accepted, both deliberate tightenings with no
	// legitimate producer: control characters in the path (the flag value
	// travels through line-oriented CI files), and a doubled slash directly
	// after the dist dir ("dist//x"), whose suffix reads as absolute.
	if !pathsafe.Relative(strings.TrimPrefix(path, distDir+"/")) {
		return fmt.Errorf("release images: ledger path must be a safe relative path under %s/ without '..': %s: %w", distDir, path, errs.ErrUsage)
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
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" || strings.Contains(raw, "://") {
		return raw
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
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")

	raw = strings.Trim(raw, "/")
	if slash := strings.Index(raw, "/"); slash >= 0 {
		raw = raw[:slash]
	}

	if raw == "" {
		return "", fmt.Errorf("release images: registry host is empty: %w", errs.ErrUsage)
	}

	return raw, nil
}
