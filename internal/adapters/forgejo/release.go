// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"code.gitea.io/sdk/gitea"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// CreateRelease creates (or replaces) a Forgejo release for the tag and
// uploads the declared assets. Create-or-replace: an existing release at
// the same tag is deleted first so a re-run publishes cleanly (the tag
// itself is kept), mirroring the github adapter's cleanup.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("CreateRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("CreateRelease: repo is empty: %w", errs.ErrUsage)
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return err
	}

	if err = replaceExistingRelease(client, owner, name, spec.Tag); err != nil {
		return err
	}

	rel, resp, err := client.CreateRelease(owner, name, releaseOption(spec))
	if err != nil {
		return fmt.Errorf("forgejo create release: %w", classifyErr(resp, err))
	}

	for _, asset := range spec.Assets {
		if err := uploadAttachment(client, owner, name, rel.ID, asset); err != nil {
			return err
		}
	}

	return nil
}

// replaceExistingRelease deletes a release already present at the tag so
// a re-run publishes cleanly (the tag itself is kept). A missing release
// is not an error — there is simply nothing to replace.
func replaceExistingRelease(client *gitea.Client, owner, repo, tag string) error {
	existing, _, err := client.GetReleaseByTag(owner, repo, tag)
	if err != nil || existing == nil {
		return nil //nolint:nilerr // a missing/unreadable release at this tag means there is nothing to replace — not a failure.
	}

	if _, delErr := client.DeleteRelease(owner, repo, existing.ID); delErr != nil {
		return fmt.Errorf("forgejo delete existing release %q: %w", tag, delErr)
	}

	return nil
}

// releaseOption builds the SDK create-release options, reading the notes
// body from spec.NotesFile when set (falling back to spec.Name).
func releaseOption(spec provider.ReleaseSpec) gitea.CreateReleaseOption {
	note := spec.Name
	if spec.NotesFile != "" {
		if body, err := os.ReadFile(spec.NotesFile); err == nil {
			note = string(body)
		}
	}

	return gitea.CreateReleaseOption{
		TagName:      spec.Tag,
		Title:        cmp.Or(spec.Name, spec.Tag),
		Note:         note,
		IsDraft:      spec.Draft,
		IsPrerelease: spec.Prerelease,
	}
}

// UploadReleaseAsset uploads a single file onto the release identified by
// tag. The repository comes from the runner context since the
// ReleaseAssetUploader interface carries only tag + file.
func (p *Provider) UploadReleaseAsset(ctx context.Context, tag, file string) error {
	if tag == "" {
		return fmt.Errorf("UploadReleaseAsset: tag is empty: %w", errs.ErrUsage)
	}

	owner, name, err := p.repoFromEnv()
	if err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return err
	}

	rel, resp, err := client.GetReleaseByTag(owner, name, tag)
	if err != nil {
		return fmt.Errorf("forgejo get release %q: %w", tag, classifyErr(resp, err))
	}

	return uploadAttachment(client, owner, name, rel.ID, file)
}

// uploadAttachment streams one file to a release as an attachment.
func uploadAttachment(client *gitea.Client, owner, repo string, releaseID int64, file string) error {
	//nolint:gosec,varnamelen // G304: asset path is an operator-supplied release artifact, not attacker-controlled; f is an idiomatic file handle.
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("open asset %q: %w", file, err)
	}

	defer func() { _ = f.Close() }()

	if _, resp, err := client.CreateReleaseAttachment(owner, repo, releaseID, f, filepath.Base(file)); err != nil {
		return fmt.Errorf("forgejo upload asset %q: %w", filepath.Base(file), classifyErr(resp, err))
	}

	return nil
}
