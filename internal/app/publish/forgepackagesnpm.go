// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// NPMPublishOps is the streaming npm-adapter surface the forge-packages npm
// publish needs (distinct from the capturing NPMOps used by the validators).
type NPMPublishOps interface {
	RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error
}

// ForgePackagesNPMPublishInput drives ForgePackagesNPMPublish.
type ForgePackagesNPMPublishInput struct {
	// WorkingDir is the directory holding the packed *.tgz tarball.
	WorkingDir string
}

// ForgePackagesNPMPublish is the npm sibling of ForgePackagesDeploy: it
// publishes the packed tarball to the active forge's npm registry (GitHub
// Packages, the GitLab npm registry, or Forgejo packages). The per-forge
// registry + token come from the provider role; this use case writes a
// credentialed .npmrc to a 0600 temp file (token never on argv) and runs
// `npm publish <tarball> --userconfig <npmrc>`.
func ForgePackagesNPMPublish(ctx context.Context, ops NPMPublishOps, resolver provider.ForgeNPMRegistryResolver, stdout, stderr io.Writer, in ForgePackagesNPMPublishInput) error {
	reg, err := resolver.ResolveForgeNPMRegistry()
	if err != nil {
		return err
	}

	dir := in.WorkingDir
	if dir == "" {
		dir = "."
	}

	tarball, err := findSingleTarball(dir)
	if err != nil {
		return err
	}

	npmrcPath, cleanup, err := writeTempSecretFile("reusable-ci-npmrc-*", reg.RenderNPMRC())
	if err != nil {
		return err
	}

	defer cleanup()

	_, _ = fmt.Fprintf(stdout, "Publishing %s to the forge npm registry: %s\n", filepath.Base(tarball), reg.Registry)

	return ops.RunInherit(ctx, dir, stdout, stderr, "publish", tarball, "--userconfig", npmrcPath)
}

// findSingleTarball returns the single *.tgz in dir (the npm-pack output),
// erroring when none or more than one is present so the publish target is
// unambiguous.
func findSingleTarball(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tgz"))
	if err != nil {
		return "", fmt.Errorf("scan %q for npm tarball: %w", dir, err)
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no *.tgz tarball in %q (run `npm pack` first): %w", dir, errs.ErrMissingInput)
	default:
		return "", fmt.Errorf("%d *.tgz tarballs in %q (expected exactly one): %w", len(matches), dir, errs.ErrValidation)
	}
}
