// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// CreateRelease creates a release via the GitHub REST API
// (POST /repos/{owner}/{repo}/releases). Semantics:
//
//   - existing draft/prerelease tag → delete-and-recreate
//   - existing stable tag → error ("Cannot overwrite")
//
// Assets are uploaded after the release exists via UploadReleaseAsset.
// The HTTP transport is the shared retry-aware client, so transient
// 502/503/429 responses become silent retries with backoff instead of
// the release step failing wholesale.
//
//nolint:cyclop // REST flow: ensure tag → cleanup existing → create release → upload assets.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("CreateRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("CreateRelease: repo is empty: %w", errs.ErrUsage)
	}

	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return err
	}

	if cleanupErr := p.cleanupExistingRelease(ctx, client, owner, repoName, spec.Tag); cleanupErr != nil {
		return cleanupErr
	}

	body, err := readNotesFile(spec.NotesFile)
	if err != nil {
		return err
	}

	rel, _, err := client.Repositories.CreateRelease(ctx, owner, repoName, releaseRequest(spec, body))
	if err != nil {
		return fmt.Errorf("create release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	releaseID := rel.GetID()
	for _, asset := range spec.Assets {
		if err := p.uploadAsset(ctx, client, owner, repoName, releaseID, asset); err != nil {
			return err
		}
	}

	return nil
}

// releaseRequest builds the GitHub release payload shared by CreateRelease
// (delete-and-recreate) and PublishRelease (in-place upsert), so the two
// strategies never drift in how they map a ReleaseSpec onto the API fields.
func releaseRequest(spec provider.ReleaseSpec, body string) *gogithub.RepositoryRelease {
	name := spec.Name
	if name == "" {
		name = spec.Tag
	}

	makeLatest := string(spec.MakeLatest)
	if makeLatest == "" {
		makeLatest = string(provider.MakeLatestTrue)
	}

	req := &gogithub.RepositoryRelease{
		TagName:    gogithub.Ptr(spec.Tag),
		Name:       gogithub.Ptr(name),
		Draft:      gogithub.Ptr(spec.Draft),
		Prerelease: gogithub.Ptr(spec.Prerelease),
		MakeLatest: gogithub.Ptr(makeLatest),
	}
	if body != "" {
		req.Body = gogithub.Ptr(body)
	}

	return req
}

// PublishRelease creates the release for spec.Tag if it is missing, or updates
// it and reconciles its assets in place otherwise. Unlike CreateRelease it
// never deletes the release object: an existing release (draft, prerelease, or
// stable) is edited in place, colliding assets are replaced by basename,
// desired assets are uploaded, and assets no longer in spec.Assets are removed.
// It satisfies the provider.ReleasePublisher role.
func (p *Provider) PublishRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("PublishRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("PublishRelease: repo is empty: %w", errs.ErrUsage)
	}

	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return err
	}

	body, err := readNotesFile(spec.NotesFile)
	if err != nil {
		return err
	}

	releaseID, err := p.upsertRelease(ctx, client, owner, repoName, spec, body)
	if err != nil {
		return err
	}

	return p.reconcileReleaseAssets(ctx, client, owner, repoName, releaseID, spec.Assets)
}

// upsertRelease returns the id of the release for spec.Tag, creating it when
// absent (404) and otherwise editing it in place — never deleting it.
func (p *Provider) upsertRelease(ctx context.Context, client *gogithub.Client, owner, repo string, spec provider.ReleaseSpec, body string) (int64, error) {
	req := releaseRequest(spec, body)

	existing, resp, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, spec.Tag)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			rel, _, createErr := client.Repositories.CreateRelease(ctx, owner, repo, req)
			if createErr != nil {
				return 0, fmt.Errorf("create release %q: %w", spec.Tag, classifyGitHubError(createErr))
			}

			return rel.GetID(), nil
		}

		return 0, fmt.Errorf("look up release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	rel, _, err := client.Repositories.EditRelease(ctx, owner, repo, existing.GetID(), req)
	if err != nil {
		return 0, fmt.Errorf("update release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	return rel.GetID(), nil
}

// reconcileReleaseAssets makes the release's asset set match desired: each
// desired file is uploaded (clobbering a same-name asset), then any remaining
// asset whose basename is not in desired is deleted.
func (p *Provider) reconcileReleaseAssets(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64, desired []string) error {
	for _, asset := range desired {
		if err := p.uploadAsset(ctx, client, owner, repo, releaseID, asset); err != nil {
			return err
		}
	}

	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list assets for reconcile: %w", err)
	}

	desiredNames := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		desiredNames[filepath.Base(file)] = struct{}{}
	}

	for _, existing := range assets {
		if _, keep := desiredNames[existing.GetName()]; keep {
			continue
		}

		if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, existing.GetID()); err != nil {
			return fmt.Errorf("delete stale asset %q: %w", existing.GetName(), classifyGitHubError(err))
		}
	}

	return nil
}

// UploadReleaseAsset uploads file to the release identified by tag.
// `--clobber` semantics: when an asset with the same basename already
// exists on the release, it is deleted before the upload, so the new
// file lands at the same name.
func (p *Provider) UploadReleaseAsset(ctx context.Context, tag, file string) error {
	if tag == "" {
		return fmt.Errorf("UploadReleaseAsset: tag is empty: %w", errs.ErrUsage)
	}

	if file == "" {
		return fmt.Errorf("UploadReleaseAsset: file is empty: %w", errs.ErrUsage)
	}

	owner, repoName, err := p.repoFromEnv()
	if err != nil {
		return err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return err
	}

	rel, _, err := client.Repositories.GetReleaseByTag(ctx, owner, repoName, tag)
	if err != nil {
		return fmt.Errorf("look up release %q: %w", tag, classifyGitHubError(err))
	}

	return p.uploadAsset(ctx, client, owner, repoName, rel.GetID(), file)
}

// uploadAsset uploads one file to the given release, first deleting
// any existing asset with the same basename.
func (p *Provider) uploadAsset(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64, file string) error {
	name := filepath.Base(file)

	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list assets for clobber check: %w", err)
	}

	for _, a := range assets {
		if a.GetName() == name {
			if _, delErr := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, a.GetID()); delErr != nil {
				return fmt.Errorf("delete existing asset %q before re-upload: %w", name, classifyGitHubError(delErr))
			}

			break
		}
	}

	f, err := os.Open(file) //nolint:gosec,varnamelen // caller-controlled path; explicit asset list
	if err != nil {
		return fmt.Errorf("open release asset %q: %w", file, err)
	}

	defer func() { _ = f.Close() }()

	if _, _, err := client.Repositories.UploadReleaseAsset(ctx, owner, repo, releaseID, &gogithub.UploadOptions{Name: name}, f); err != nil {
		return fmt.Errorf("upload asset %q: %w", file, classifyGitHubError(err))
	}

	return nil
}

// listAllReleaseAssets returns every asset on a release, paging
// through the result set so releases with >100 assets are handled.
func listAllReleaseAssets(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64) ([]*gogithub.ReleaseAsset, error) {
	opts := &gogithub.ListOptions{PerPage: 100}

	var all []*gogithub.ReleaseAsset

	for {
		page, resp, err := client.Repositories.ListReleaseAssets(ctx, owner, repo, releaseID, opts)
		if err != nil {
			return nil, err
		}

		all = append(all, page...)

		if resp == nil || resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return all, nil
}

// cleanupExistingRelease: if a release for tag already exists and is
// draft or prerelease, delete it; if it's stable, error out; missing
// release (404) → no-op.
func (p *Provider) cleanupExistingRelease(ctx context.Context, client *gogithub.Client, owner, repo, tag string) error {
	rel, resp, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil
		}

		return fmt.Errorf("check existing release %q: %w", tag, classifyGitHubError(err))
	}

	if rel.GetDraft() || rel.GetPrerelease() {
		if _, err := client.Repositories.DeleteRelease(ctx, owner, repo, rel.GetID()); err != nil {
			return fmt.Errorf("delete existing draft/prerelease tag %q: %w", tag, classifyGitHubError(err))
		}

		return nil
	}

	return fmt.Errorf("release %s already exists and is not a draft/prerelease; cannot overwrite: %w", tag, errs.ErrValidation)
}

// readNotesFile reads the release-notes file body, returning "" when
// the file path is empty.
func readNotesFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	body, err := os.ReadFile(path) //nolint:gosec // notes path is a CLI-flag value.
	if err != nil {
		return "", fmt.Errorf("read notes file %q: %w", path, err)
	}

	return string(body), nil
}

// splitRepo splits an "owner/repo" identifier. GitHub Enterprise's
// nested-group form is not supported on github.com so a single slash
// is the only correct shape.
func splitRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo %q must be owner/name: %w", repo, errs.ErrUsage)
	}

	return parts[0], parts[1], nil
}

// repoFromEnv extracts owner/repo from GITHUB_REPOSITORY; tests that
// run UploadReleaseAsset standalone (without ResolveContext) get it
// straight from env.
func (p *Provider) repoFromEnv() (string, string, error) {
	return splitRepo(p.envFunc()("GITHUB_REPOSITORY"))
}
