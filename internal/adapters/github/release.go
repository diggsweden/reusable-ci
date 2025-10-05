// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// CreateRelease creates a release via the GitHub REST API
// (POST /repos/{owner}/{repo}/releases). Semantics:
//
//   - existing draft/prerelease tag → delete-and-recreate
//   - existing stable tag → exact metadata and asset-digest match is success;
//     every mismatch is refused without mutation
//
// Assets are uploaded to a draft. A non-draft release becomes visible only
// after every asset upload succeeds. A failed upload leaves a draft that the
// next recreate attempt discovers through the authenticated release list.
// Publishing a staged prerelease intentionally emits GitHub's "published"
// release event rather than exposing incomplete assets for a "prereleased"
// event.
//
//nolint:cyclop // REST flow: ensure tag → cleanup existing → create draft → upload assets → publish.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if err := validateReleaseAssets(spec.Assets); err != nil {
		return err
	}

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

	body, err := readNotesFile(spec.NotesFile)
	if err != nil {
		return err
	}

	reused, cleanupErr := p.cleanupExistingRelease(ctx, client, owner, repoName, spec, body)
	if cleanupErr != nil {
		return cleanupErr
	}

	if reused {
		return nil
	}

	staged := spec
	staged.Draft = true
	staged.MakeLatest = provider.MakeLatestFalse

	release, _, err := client.Repositories.CreateRelease(ctx, owner, repoName, releaseRequest(staged, body))
	if err != nil {
		return fmt.Errorf("create release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	releaseID := release.GetID()
	for _, asset := range spec.Assets {
		if err := p.uploadAsset(ctx, client, owner, repoName, releaseID, asset); err != nil {
			return err
		}
	}

	if spec.Draft {
		return nil
	}

	return p.publishStagedRelease(ctx, client, owner, repoName, releaseID, spec, body)
}

// releaseRequest builds the GitHub release payload shared by CreateRelease
// (draft-first recreate) and PublishRelease (in-place upsert), so the two
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
// For an existing release the assets are reconciled before the metadata is
// written, as the Forgejo adapter does, so a failed upload leaves the previous
// notes and visibility rather than publishing a draft without its artifacts.
// It satisfies the provider.ReleasePublisher role.
func (p *Provider) PublishRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if err := validateReleaseAssets(spec.Assets); err != nil {
		return err
	}

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

	return p.upsertReleaseWithAssets(ctx, client, owner, repoName, spec, body)
}

// upsertReleaseWithAssets creates the release with its assets when the tag has
// none, or reconciles an existing release's assets before writing its metadata.
func (p *Provider) upsertReleaseWithAssets(ctx context.Context, client *gogithub.Client, owner, repoName string, spec provider.ReleaseSpec, body string) error {
	existing, err := findReleaseByTag(ctx, client, owner, repoName, spec.Tag)
	if err != nil {
		return err
	}

	if existing == nil {
		created, _, createErr := client.Repositories.CreateRelease(ctx, owner, repoName, releaseRequest(spec, body))
		if createErr != nil {
			return fmt.Errorf("create release %q: %w", spec.Tag, classifyGitHubError(createErr))
		}

		return p.reconcileReleaseAssets(ctx, client, owner, repoName, created.GetID(), spec.Assets)
	}

	if err := p.reconcileReleaseAssets(ctx, client, owner, repoName, existing.GetID(), spec.Assets); err != nil {
		return err
	}

	if _, _, err := client.Repositories.EditRelease(ctx, owner, repoName, existing.GetID(), releaseRequest(spec, body)); err != nil {
		return fmt.Errorf("update release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	return nil
}

func validateReleaseAssets(files []string) error {
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		if seen[name] {
			return fmt.Errorf("duplicate release asset basename: %w", errs.ErrValidation)
		}

		seen[name] = true

		root, err := pathsafe.OpenRoot(filepath.Dir(file))
		if err != nil {
			return err
		}

		info, err := root.Lstat(name)
		_ = root.Close()

		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("release asset must be a regular file: %w", errs.ErrValidation)
		}
	}

	return nil
}

// findReleaseByTag returns the release for tag, or nil when there is none. It
// reads the authenticated release list because GitHub's tag endpoint omits
// drafts, and a draft it missed would be duplicated rather than updated. Two
// releases on one tag are refused: which of them to edit is not a guess this
// adapter makes.
func findReleaseByTag(ctx context.Context, client *gogithub.Client, owner, repo, tag string) (*gogithub.RepositoryRelease, error) {
	releases, err := listAllRepositoryReleases(ctx, client, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("look up release %q: %w", tag, classifyGitHubError(err))
	}

	var found *gogithub.RepositoryRelease

	for _, release := range releases {
		if release.GetTagName() != tag {
			continue
		}

		if found != nil {
			return nil, fmt.Errorf("releases %d and %d both use tag %q; refusing to guess which to update: %w",
				found.GetID(), release.GetID(), tag, errs.ErrValidation)
		}

		found = release
	}

	return found, nil
}

// reconcileReleaseAssets makes the release's asset set match desired: each
// desired file is uploaded (clobbering a same-name asset), then any remaining
// asset whose basename is not in desired is deleted.
func (p *Provider) reconcileReleaseAssets(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64, desired []string) error {
	desiredNames := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		name := filepath.Base(file)
		desiredNames[name] = struct{}{}
	}

	ordered := append([]string(nil), desired...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return releaseAssetPriority(filepath.Base(ordered[i])) < releaseAssetPriority(filepath.Base(ordered[j]))
	})

	for _, asset := range ordered {
		if err := p.uploadAsset(ctx, client, owner, repo, releaseID, asset); err != nil {
			return err
		}
	}

	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list assets for reconcile: %w", err)
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

func releaseAssetPriority(name string) int {
	switch name {
	case "checksums.txt.bundle":
		return 1
	case "checksums.txt":
		return 2
	default:
		return 0
	}
}

// UploadReleaseAsset uploads file to the release identified by tag. A
// same-named asset is replaced through the verified staging flow below, so a
// failed upload does not delete the known-good object first.
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

// uploadAsset replaces a same-named asset without deleting the known-good
// object first. A colliding upload is staged under a unique name, verified,
// promoted, and only then is the renamed backup removed.
//
//nolint:cyclop // verified direct upload and staged replacement share one transaction.
func (p *Provider) uploadAsset(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64, file string) error {
	name := filepath.Base(file)

	assetFile, size, digest, err := openReleaseAsset(file)
	if err != nil {
		return err
	}

	defer func() { _ = assetFile.Close() }()

	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list assets for clobber check: %w", err)
	}

	var existing *gogithub.ReleaseAsset

	for _, a := range assets {
		if a.GetName() == name {
			existing = a

			break
		}
	}

	if existing == nil {
		_, uploadErr := uploadVerifiedReleaseAsset(ctx, client, owner, repo, releaseID, name, assetFile, size, digest, nil)

		return uploadErr
	}

	stagedName, err := temporaryReleaseAssetName(name, "upload")
	if err != nil {
		return err
	}

	staged, err := uploadVerifiedReleaseAsset(ctx, client, owner, repo, releaseID, stagedName, assetFile, size, digest, nil)
	if err != nil {
		return err
	}

	backupName, err := temporaryReleaseAssetName(name, "backup")
	if err != nil {
		_, _ = client.Repositories.DeleteReleaseAsset(ctx, owner, repo, staged.GetID())

		return err
	}

	if _, _, err := client.Repositories.EditReleaseAsset(ctx, owner, repo, existing.GetID(), &gogithub.ReleaseAsset{Name: gogithub.Ptr(backupName)}); err != nil {
		if _, cleanupErr := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, staged.GetID()); cleanupErr != nil {
			return fmt.Errorf("stage existing asset %q as backup: %w; cleanup failed: %w; known-good asset id %d may be named %q or %q; staged asset id %d at %q remains", name, classifyGitHubError(err), classifyGitHubError(cleanupErr), existing.GetID(), name, backupName, staged.GetID(), stagedName)
		}

		return fmt.Errorf("stage existing asset %q as backup: %w; known-good asset id %d may be named %q or %q", name, classifyGitHubError(err), existing.GetID(), name, backupName)
	}

	if _, _, err := client.Repositories.EditReleaseAsset(ctx, owner, repo, staged.GetID(), &gogithub.ReleaseAsset{Name: gogithub.Ptr(name)}); err != nil {
		_, _, restoreErr := client.Repositories.EditReleaseAsset(ctx, owner, repo, existing.GetID(), &gogithub.ReleaseAsset{Name: gogithub.Ptr(name)})
		if restoreErr != nil {
			return fmt.Errorf("promote staged asset %q: %w; restore failed: %w; retained known-good asset id %d (last confirmed name %q) and staged asset id %d (last confirmed name %q) for recovery; rename outcomes are unconfirmed", name, classifyGitHubError(err), classifyGitHubError(restoreErr), existing.GetID(), backupName, staged.GetID(), stagedName)
		}

		if _, cleanupErr := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, staged.GetID()); cleanupErr != nil {
			return fmt.Errorf("promote staged asset %q: %w; cleanup failed: %w; original restored, staged asset id %d at %q remains", name, classifyGitHubError(err), classifyGitHubError(cleanupErr), staged.GetID(), stagedName)
		}

		return fmt.Errorf("promote staged asset %q: %w", name, classifyGitHubError(err))
	}

	if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, existing.GetID()); err != nil {
		return fmt.Errorf("delete replaced asset backup %q: %w", backupName, classifyGitHubError(err))
	}

	return nil
}

func openReleaseAsset(path string) (*os.File, int64, string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, 0, "", fmt.Errorf("stat release asset %q: %w", path, err)
	}

	if !before.Mode().IsRegular() {
		return nil, 0, "", fmt.Errorf("release asset %q is not a regular file: %w", path, errs.ErrValidation)
	}

	file, err := os.Open(path) //nolint:gosec // explicit caller-selected release asset.
	if err != nil {
		return nil, 0, "", fmt.Errorf("open release asset %q: %w", path, err)
	}

	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()

		if err != nil {
			return nil, 0, "", fmt.Errorf("stat opened release asset %q: %w", path, err)
		}

		return nil, 0, "", fmt.Errorf("release asset %q changed while opening: %w", path, errs.ErrValidation)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		_ = file.Close()

		return nil, 0, "", fmt.Errorf("hash release asset %q: %w", path, err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()

		return nil, 0, "", fmt.Errorf("rewind release asset %q: %w", path, err)
	}

	return file, opened.Size(), "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

//nolint:nestif // verification failure cleanup must remain adjacent to the failed upload.
func uploadVerifiedReleaseAsset(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	releaseID int64,
	name string,
	file *os.File,
	size int64,
	digest string,
	beforeCleanup func() error,
) (*gogithub.ReleaseAsset, error) {
	asset, _, err := client.Repositories.UploadReleaseAsset(ctx, owner, repo, releaseID, &gogithub.UploadOptions{Name: name}, file)
	if err != nil {
		return nil, fmt.Errorf("upload asset %q: %w", name, classifyGitHubError(err))
	}

	if asset.GetID() == 0 || int64(asset.GetSize()) != size || asset.GetDigest() != digest {
		if asset.GetID() != 0 {
			if beforeCleanup != nil {
				if err := beforeCleanup(); err != nil {
					return nil, fmt.Errorf("uploaded asset %q failed verification and source freshness blocked cleanup: %w", name, err)
				}
			}

			if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, asset.GetID()); err != nil {
				return nil, fmt.Errorf("uploaded asset %q failed verification and cleanup: %w", name, classifyGitHubError(err))
			}
		}

		return nil, fmt.Errorf("uploaded asset %q failed size/digest verification: %w", name, errs.ErrValidation)
	}

	return asset, nil
}

func temporaryReleaseAssetName(name, purpose string) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate temporary release asset name: %w", err)
	}

	return fmt.Sprintf("%s.reusable-ci-%s-%x", name, purpose, suffix), nil
}

// listAllReleaseAssets returns every asset on a release, paging
// through the result set so releases with >100 assets are handled.
func listAllReleaseAssets(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64) ([]*gogithub.ReleaseAsset, error) {
	return listPages(func(opts *gogithub.ListOptions) ([]*gogithub.ReleaseAsset, *gogithub.Response, error) {
		return client.Repositories.ListReleaseAssets(ctx, owner, repo, releaseID, opts)
	})
}

func listAllRepositoryReleases(ctx context.Context, client *gogithub.Client, owner, repo string) ([]*gogithub.RepositoryRelease, error) {
	return listPages(func(opts *gogithub.ListOptions) ([]*gogithub.RepositoryRelease, *gogithub.Response, error) {
		return client.Repositories.ListReleases(ctx, owner, repo, opts)
	})
}

// cleanupExistingRelease uses the authenticated list because GitHub's tag
// endpoint only returns published releases. Drafts and prereleases retain the
// recreate strategy's replacement semantics. A stable release is immutable: an
// exact metadata/asset-digest match is an idempotent success, and any mismatch
// is refused without deleting or overwriting it.
func (p *Provider) cleanupExistingRelease(ctx context.Context, client *gogithub.Client, owner, repo string, spec provider.ReleaseSpec, body string) (bool, error) {
	releases, err := listAllRepositoryReleases(ctx, client, owner, repo)
	if err != nil {
		return false, fmt.Errorf("list releases before creating %q: %w", spec.Tag, classifyGitHubError(err))
	}

	var matching []*gogithub.RepositoryRelease

	for _, release := range releases {
		if release.GetTagName() != spec.Tag {
			continue
		}

		if !release.GetDraft() && !release.GetPrerelease() {
			if err := exactStableReleaseMatches(ctx, client, owner, repo, release, spec, body); err != nil {
				return false, err
			}

			return true, nil
		}

		matching = append(matching, release)
	}

	for _, release := range matching {
		if _, err := client.Repositories.DeleteRelease(ctx, owner, repo, release.GetID()); err != nil {
			return false, fmt.Errorf("delete existing draft/prerelease tag %q: %w", spec.Tag, classifyGitHubError(err))
		}
	}

	return false, nil
}

//nolint:cyclop // stable-release immutability checks metadata and every exact asset.
func exactStableReleaseMatches(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	release *gogithub.RepositoryRelease,
	spec provider.ReleaseSpec,
	body string,
) error {
	if !releaseMetadataMatches(release, spec, body) {
		return fmt.Errorf("stable release %s already exists with different metadata; refusing to overwrite it: %w", spec.Tag, errs.ErrValidation)
	}

	assets, err := listAllReleaseAssets(ctx, client, owner, repo, release.GetID())
	if err != nil {
		return fmt.Errorf("list stable release %s assets: %w", spec.Tag, classifyGitHubError(err))
	}

	if len(assets) != len(spec.Assets) {
		return fmt.Errorf("stable release %s has %d assets, requested release has %d; refusing to overwrite it: %w", spec.Tag, len(assets), len(spec.Assets), errs.ErrValidation)
	}

	existing := make(map[string]*gogithub.ReleaseAsset, len(assets))
	for _, asset := range assets {
		if _, duplicate := existing[asset.GetName()]; duplicate {
			return fmt.Errorf("stable release %s has duplicate asset %q: %w", spec.Tag, asset.GetName(), errs.ErrValidation)
		}

		existing[asset.GetName()] = asset
	}

	seen := make(map[string]struct{}, len(spec.Assets))
	for _, path := range spec.Assets {
		name := filepath.Base(path)
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("requested stable release %s has duplicate asset basename %q: %w", spec.Tag, name, errs.ErrValidation)
		}

		seen[name] = struct{}{}

		file, size, digest, err := openReleaseAsset(path)
		if err != nil {
			return err
		}

		_ = file.Close()

		asset := existing[name]
		if asset == nil || int64(asset.GetSize()) != size || asset.GetDigest() != digest {
			return fmt.Errorf("stable release %s asset %q does not match requested size and digest; refusing to overwrite it: %w", spec.Tag, name, errs.ErrValidation)
		}
	}

	return nil
}

func (p *Provider) publishStagedRelease(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	releaseID int64,
	spec provider.ReleaseSpec,
	body string,
) error {
	_, _, err := client.Repositories.EditRelease(ctx, owner, repo, releaseID, releaseRequest(spec, body))
	if err == nil {
		return nil
	}

	publishErr := classifyGitHubError(err)

	release, _, getErr := client.Repositories.GetRelease(ctx, owner, repo, releaseID)
	if getErr == nil && releaseMetadataMatches(release, spec, body) {
		return nil
	}

	return fmt.Errorf("publish release %q: %w", spec.Tag, publishErr)
}

func releaseMetadataMatches(release *gogithub.RepositoryRelease, spec provider.ReleaseSpec, body string) bool {
	name := spec.Name
	if name == "" {
		name = spec.Tag
	}

	return release.GetTagName() == spec.Tag &&
		release.GetName() == name &&
		release.GetBody() == body &&
		release.GetDraft() == spec.Draft &&
		release.GetPrerelease() == spec.Prerelease
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

// splitRepo requires exactly two path-safe GitHub identifier segments.
func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo %q must be owner/name: %w", repo, errs.ErrUsage)
	}

	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsFunc(part, func(r rune) bool {
			return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", r)
		}) {
			return "", "", fmt.Errorf("repository must have two path-safe identifier segments: %w", errs.ErrUsage)
		}
	}

	return parts[0], parts[1], nil
}

// repoFromEnv extracts owner/repo from GITHUB_REPOSITORY; tests that
// run UploadReleaseAsset standalone (without ResolveContext) get it
// straight from env.
func (p *Provider) repoFromEnv() (string, string, error) {
	return splitRepo(p.envFunc()("GITHUB_REPOSITORY"))
}
