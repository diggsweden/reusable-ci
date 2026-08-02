// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
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

// PublishRelease creates or updates a Forgejo release in place and reconciles
// assets by basename. Existing releases are updated via PATCH, never deleted; colliding
// assets are deleted before upload and stale assets are deleted after all
// desired assets upload successfully.
func (p *Provider) PublishRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error { //nolint:cyclop // release upsert + asset reconciliation is one API transaction shape.
	if spec.Tag == "" {
		return fmt.Errorf("PublishRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("PublishRelease: repo is empty: %w", errs.ErrUsage)
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return err
	}

	existing, resp, err := client.GetReleaseByTag(owner, name, spec.Tag)
	if err != nil {
		if responseStatus(resp) == http.StatusNotFound {
			return createReleaseWithAssets(client, owner, name, spec)
		}

		return fmt.Errorf("forgejo get release %q: %w", spec.Tag, classifyErr(resp, err))
	}

	if existing == nil || existing.ID == 0 {
		return fmt.Errorf("existing release response did not include id for %q: %w", spec.Tag, errs.ErrMalformedInput)
	}

	edit, err := editReleaseOption(spec)
	if err != nil {
		return err
	}

	if _, resp, err = client.EditRelease(owner, name, existing.ID, edit); err != nil {
		return fmt.Errorf("forgejo update release %q: %w", spec.Tag, classifyErr(resp, err))
	}

	attachments, err := listReleaseAttachments(client, owner, name, existing.ID)
	if err != nil {
		return err
	}

	return reconcileReleaseAttachments(client, owner, name, existing.ID, attachments, spec.Assets)
}

func createReleaseWithAssets(client *gitea.Client, owner, name string, spec provider.ReleaseSpec) error {
	opt, err := strictReleaseOption(spec)
	if err != nil {
		return err
	}

	rel, resp, err := client.CreateRelease(owner, name, opt)
	if err != nil {
		return fmt.Errorf("forgejo create release: %w", classifyErr(resp, err))
	}

	if rel == nil || rel.ID == 0 {
		return fmt.Errorf("created release response did not include id for %q: %w", spec.Tag, errs.ErrMalformedInput)
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
	note, err := releaseNoteBody(spec)
	if err != nil {
		note = spec.Name
	}

	return gitea.CreateReleaseOption{
		TagName:      spec.Tag,
		Title:        cmp.Or(spec.Name, spec.Tag),
		Note:         note,
		IsDraft:      spec.Draft,
		IsPrerelease: spec.Prerelease,
	}
}

func strictReleaseOption(spec provider.ReleaseSpec) (gitea.CreateReleaseOption, error) {
	note, err := releaseNoteBody(spec)
	if err != nil {
		return gitea.CreateReleaseOption{}, err
	}

	return gitea.CreateReleaseOption{
		TagName:      spec.Tag,
		Title:        cmp.Or(spec.Name, spec.Tag),
		Note:         note,
		IsDraft:      spec.Draft,
		IsPrerelease: spec.Prerelease,
	}, nil
}

func editReleaseOption(spec provider.ReleaseSpec) (gitea.EditReleaseOption, error) {
	note, err := releaseNoteBody(spec)
	if err != nil {
		return gitea.EditReleaseOption{}, err
	}

	return gitea.EditReleaseOption{
		TagName:      spec.Tag,
		Title:        cmp.Or(spec.Name, spec.Tag),
		Note:         note,
		IsDraft:      boolPtr(spec.Draft),
		IsPrerelease: boolPtr(spec.Prerelease),
	}, nil
}

func releaseNoteBody(spec provider.ReleaseSpec) (string, error) {
	note := spec.Name
	if spec.NotesFile == "" {
		return note, nil
	}

	body, err := os.ReadFile(spec.NotesFile) //nolint:gosec // release notes path is validated by the use case before provider mutation.
	if err != nil {
		return "", fmt.Errorf("read release notes %q: %w", spec.NotesFile, err)
	}

	return string(body), nil
}

func boolPtr(v bool) *bool { return &v }

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

func listReleaseAttachments(client *gitea.Client, owner, repo string, releaseID int64) ([]*gitea.Attachment, error) {
	opt := gitea.ListReleaseAttachmentsOptions{ListOptions: gitea.ListOptions{Page: 1, PageSize: 100}}

	var all []*gitea.Attachment

	for {
		attachments, resp, err := client.ListReleaseAttachments(owner, repo, releaseID, opt)
		if err != nil {
			return nil, fmt.Errorf("forgejo list release assets: %w", classifyErr(resp, err))
		}

		all = append(all, attachments...)

		if resp == nil || resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	return all, nil
}

func reconcileReleaseAttachments(client *gitea.Client, owner, repo string, releaseID int64, existing []*gitea.Attachment, desired []string) error {
	desiredNames := map[string]struct{}{}
	for _, asset := range desired {
		desiredNames[filepath.Base(asset)] = struct{}{}
	}

	for _, asset := range desired {
		name := filepath.Base(asset)
		for _, attachment := range existing {
			if attachment.Name != name {
				continue
			}

			if err := deleteReleaseAttachment(client, owner, repo, releaseID, attachment, "existing"); err != nil {
				return err
			}
		}

		if err := uploadAttachment(client, owner, repo, releaseID, asset); err != nil {
			return err
		}
	}

	for _, attachment := range existing {
		if _, keep := desiredNames[attachment.Name]; keep {
			continue
		}

		if err := deleteReleaseAttachment(client, owner, repo, releaseID, attachment, "stale"); err != nil {
			return err
		}
	}

	return nil
}

func deleteReleaseAttachment(client *gitea.Client, owner, repo string, releaseID int64, attachment *gitea.Attachment, reason string) error {
	if attachment == nil || attachment.ID == 0 {
		return fmt.Errorf("%s release asset did not include id: %w", reason, errs.ErrMalformedInput)
	}

	if resp, err := client.DeleteReleaseAttachment(owner, repo, releaseID, attachment.ID); err != nil {
		return fmt.Errorf("forgejo delete %s release asset %q: %w", reason, attachment.Name, classifyErr(resp, err))
	}

	return nil
}

func responseStatus(resp *gitea.Response) int {
	if resp == nil || resp.Response == nil {
		return 0
	}

	return resp.StatusCode
}
