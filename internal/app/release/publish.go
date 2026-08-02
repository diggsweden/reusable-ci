// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// PublishReleaseInput drives `release publish`: an in-place release
// create/update with exact asset reconciliation.
type PublishReleaseInput struct {
	Tag                       string
	Repository                string
	ReleaseName               string
	ReleaseNameFromRepository bool
	ReleaseNotesFile          string
	Draft                     bool
	Prerelease                bool
	Assets                    []string
}

// PublishRelease validates the release notes and desired assets before asking
// the provider to create/update the release. This keeps filesystem/path policy
// outside adapters and ensures provider mutations never start when the desired
// asset set is malformed.
func PublishRelease(ctx context.Context, pub provider.ReleasePublisher, out io.Writer, in PublishReleaseInput) error { //nolint:varnamelen // provider role name is intentionally short at call sites.
	tag := strings.TrimSpace(in.Tag)
	if tag == "" {
		return fmt.Errorf("tag is required: pass --tag <name> or set $TAG_NAME: %w", errs.ErrUsage)
	}

	repo := strings.TrimSpace(in.Repository)
	if repo == "" {
		return fmt.Errorf("repository is required: pass --repository <owner/repo> or set $REPOSITORY: %w", errs.ErrUsage)
	}

	notesFile := strings.TrimSpace(in.ReleaseNotesFile)
	if notesFile == "" {
		return fmt.Errorf("release notes file is required: %w", errs.ErrUsage)
	}

	if !safeRelativePath(notesFile) {
		return fmt.Errorf("unsafe release notes path: %s: %w", notesFile, errs.ErrValidation)
	}

	if !regularReleaseFile(notesFile) {
		return fmt.Errorf("release notes are missing, not a regular file, or a symlink: %s: %w", notesFile, errs.ErrMissingInput)
	}

	assets, err := validatePublishAssets(in.Assets)
	if err != nil {
		return err
	}

	releaseName := strings.TrimSpace(in.ReleaseName)
	if releaseName == "" {
		if in.ReleaseNameFromRepository {
			releaseName = repositoryReleaseName(repo, tag)
		} else {
			releaseName = tag
		}
	}

	if out == nil {
		out = io.Discard
	}

	_, _ = fmt.Fprintf(out, "Publishing release %s with %d asset(s)\n", tag, len(assets))

	return pub.PublishRelease(ctx, repo, provider.ReleaseSpec{
		Tag:        tag,
		Name:       releaseName,
		NotesFile:  notesFile,
		Draft:      in.Draft,
		Prerelease: in.Prerelease,
		Assets:     assets,
	})
}

func repositoryReleaseName(repo, tag string) string {
	idx := strings.LastIndex(repo, "/")
	if idx >= 0 && idx+1 < len(repo) {
		return repo[idx+1:] + " " + tag
	}

	return repo + " " + tag
}

func validatePublishAssets(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no release asset files provided: %w", errs.ErrUsage)
	}

	assets := make([]string, 0, len(paths))
	seenNames := map[string]string{}

	for _, asset := range paths {
		if strings.TrimSpace(asset) == "" {
			return nil, fmt.Errorf("release asset path is empty: %w", errs.ErrUsage)
		}

		if !safeRelativePath(asset) {
			return nil, fmt.Errorf("unsafe release asset path: %s: %w", asset, errs.ErrValidation)
		}

		if !regularReleaseFile(asset) {
			return nil, fmt.Errorf("asset is missing, not a regular file, or a symlink: %s: %w", asset, errs.ErrMissingInput)
		}

		name := filepath.Base(asset)
		if previous, ok := seenNames[name]; ok {
			return nil, fmt.Errorf("duplicate release asset basename: %s (first: %s, second: %s): %w", name, previous, asset, errs.ErrValidation)
		}

		seenNames[name] = asset
		assets = append(assets, asset)
	}

	return assets, nil
}
