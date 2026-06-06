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

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
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

	name := spec.Name
	if name == "" {
		name = spec.Tag
	}

	body, err := readNotesFile(spec.NotesFile)
	if err != nil {
		return err
	}

	makeLatest := "true"
	if !spec.MakeLatest {
		makeLatest = "false"
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

	rel, _, err := client.Repositories.CreateRelease(ctx, owner, repoName, req)
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
