// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

const selfRuntimeBody = "Rolling pre-release built from the latest development commit. Not for production; pin a vN.N.N release for an immutable CLI."

// selfRuntimeCLIPublisher is intentionally not a cross-forge provider role.
// It is the narrow GitHub operation needed by this repository's own runtime
// workflow and has no second consumer.
type selfRuntimeCLIPublisher interface {
	PublishSelfRuntimeCLI(ctx context.Context, repository, sourceRef, targetSHA, body string, spec provider.ReleaseSpec) error
}

// PublishSelfRuntimeCLIInput drives the repository-private rolling CLI
// publication path.
type PublishSelfRuntimeCLIInput struct {
	Repository string
	SourceRef  string
	TargetSHA  string
	AssetsDir  string
}

// PublishSelfRuntimeCLI validates the immutable inputs and exact GoReleaser
// asset set before allowing the GitHub adapter to mutate the rolling channel.
func PublishSelfRuntimeCLI(ctx context.Context, publisher selfRuntimeCLIPublisher, out io.Writer, in PublishSelfRuntimeCLIInput) error {
	if in.Repository != domainrelease.SelfRuntimeRepository {
		return fmt.Errorf("self-runtime CLI publication requires repository %q (got %q): %w", domainrelease.SelfRuntimeRepository, in.Repository, errs.ErrValidation)
	}

	if !domaingit.ValidCommitSHA(in.TargetSHA) {
		return fmt.Errorf("self-runtime CLI target must be a full 40- or 64-character lowercase commit SHA (got %q): %w", in.TargetSHA, errs.ErrValidation)
	}

	if !domainrelease.SelfRuntimeSourceRefAllowed(in.SourceRef) {
		return fmt.Errorf("self-runtime CLI source ref is not trusted for publication (got %q): %w", in.SourceRef, errs.ErrValidation)
	}

	version := strings.TrimPrefix(domainrelease.SelfRuntimeChannelTag, "v")

	assets, err := selfRuntimeCLIAssets(in.AssetsDir, version)
	if err != nil {
		return err
	}

	spec := provider.ReleaseSpec{
		Tag:        domainrelease.SelfRuntimeChannelTag,
		Name:       "reusable-ci " + version + " (rolling pre-release)",
		Prerelease: true,
		MakeLatest: provider.MakeLatestFalse,
		Assets:     assets,
	}

	if out == nil {
		out = io.Discard
	}

	_, _ = fmt.Fprintf(out, "Publishing repository-private rolling CLI channel %s at %s with %d assets\n", domainrelease.SelfRuntimeChannelTag, in.TargetSHA, len(assets))

	return publisher.PublishSelfRuntimeCLI(ctx, in.Repository, in.SourceRef, in.TargetSHA, selfRuntimeBody, spec)
}

func selfRuntimeCLIAssets(dir, version string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("self-runtime CLI assets directory is required: %w", errs.ErrUsage)
	}

	dir = filepath.Clean(dir)
	if !pathsafe.Relative(dir) {
		return nil, fmt.Errorf("self-runtime CLI assets directory must be workspace-relative (got %q): %w", dir, errs.ErrValidation)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("self-runtime CLI assets directory %q: %w", dir, errs.ErrMissingInput)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("self-runtime CLI assets directory must be a real directory, not a file or symlink: %s: %w", dir, errs.ErrValidation)
	}

	names := make([]string, 0, 12)

	for _, goos := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			archive := fmt.Sprintf("reusable-ci_%s_%s_%s.tar.gz", version, goos, arch)
			names = append(names, archive, archive+".cdx.sbom.json")
		}
	}

	names = append(names,
		"checksums.txt",
		"checksums.txt.bundle",
		"reusable-ci.intoto.json",
		"reusable-ci.intoto.jsonl",
	)

	assets := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if !regularReleaseFile(path) {
			return nil, fmt.Errorf("required self-runtime CLI asset is missing, not a regular file, or a symlink: %s: %w", path, errs.ErrMissingInput)
		}

		assets = append(assets, path)
	}

	return assets, nil
}
