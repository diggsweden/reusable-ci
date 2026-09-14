// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// PublishSelfRuntimeCLI reconciles this repository's pre-provisioned mutable
// -pre release in place, then moves its tag only after the source ref is
// revalidated.
// It is deliberately narrower than the cross-forge release provider roles.
//
//nolint:cyclop // the rolling publication sequence intentionally remains explicit and linear.
func (p *Provider) PublishSelfRuntimeCLI(ctx context.Context, repository, sourceRef, targetSHA, body string, spec provider.ReleaseSpec) error {
	if err := validateSelfRuntimePublication(repository, sourceRef, targetSHA, spec); err != nil {
		return err
	}

	owner, repo, err := splitRepo(repository)
	if err != nil {
		return err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return err
	}

	existing, resp, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, spec.Tag)
	if err != nil && !githubResponseNotFound(resp) {
		return fmt.Errorf("look up self-runtime release %q: %w", spec.Tag, classifyGitHubError(err))
	}

	if existing != nil && !existing.GetPrerelease() {
		return fmt.Errorf("refusing to update non-prerelease release %q: %w", spec.Tag, errs.ErrValidation)
	}

	if existing == nil {
		return fmt.Errorf("self-runtime prerelease %q must be provisioned before tag-last publication: %w", spec.Tag, errs.ErrValidation)
	}

	releaseID := existing.GetID()
	fresh := func() error {
		return requireFreshGitHubSourceRef(ctx, client, owner, repo, sourceRef, targetSHA)
	}

	priorAssets, snapshotDir, err := p.snapshotSelfRuntimeAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return err
	}

	if snapshotDir != "" {
		defer func() { _ = os.RemoveAll(snapshotDir) }()
	}

	rollback := func(cause error) error {
		if restoreErr := p.restoreSelfRuntimeAssets(ctx, client, owner, repo, releaseID, priorAssets); restoreErr != nil {
			return errors.Join(cause, fmt.Errorf("restore prior self-runtime assets: %w", restoreErr))
		}

		return cause
	}

	// Metadata is updated before the asset commit. During payload replacement
	// consumers may see a mixed set, but the old checksum (or no checksum while
	// it is directly replaced) makes verification fail closed.
	req := releaseRequest(spec, body)
	req.TargetCommitish = gogithub.Ptr(targetSHA)

	if freshErr := fresh(); freshErr != nil {
		return freshErr
	}

	if _, _, editErr := client.Repositories.EditRelease(ctx, owner, repo, releaseID, req); editErr != nil {
		return fmt.Errorf("publish self-runtime release %q: %w", spec.Tag, classifyGitHubError(editErr))
	}

	if reconcileErr := p.reconcileSelfRuntimeAssets(ctx, client, owner, repo, releaseID, spec.Assets, fresh); reconcileErr != nil {
		return reconcileErr
	}

	if verifyErr := verifyExactReleaseAssets(ctx, client, owner, repo, releaseID, spec.Assets); verifyErr != nil {
		return rollback(verifyErr)
	}

	// The helper checks freshness after its tag lookup, immediately before the
	// final create/update mutation.
	if moveErr := moveGitHubTag(ctx, client, owner, repo, spec.Tag, targetSHA, fresh); moveErr != nil {
		return rollback(moveErr)
	}

	resolved, err := resolveGitHubTag(ctx, client, owner, repo, spec.Tag)
	if err != nil {
		return rollback(err)
	}

	if resolved != targetSHA {
		return rollback(fmt.Errorf("self-runtime tag %q resolves to %s, want target %s: %w", spec.Tag, resolved, targetSHA, errs.ErrValidation))
	}

	return nil
}

type selfRuntimeAssetSnapshot struct {
	name string
	path string
}

func (p *Provider) snapshotSelfRuntimeAssets(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	releaseID int64,
) ([]selfRuntimeAssetSnapshot, string, error) {
	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return nil, "", fmt.Errorf("list self-runtime assets for snapshot: %w", err)
	}

	if len(assets) == 0 {
		return nil, "", nil
	}

	dir, err := os.MkdirTemp("", "reusable-ci-self-runtime-assets-*")
	if err != nil {
		return nil, "", fmt.Errorf("create self-runtime asset snapshot: %w", err)
	}

	snapshots := make([]selfRuntimeAssetSnapshot, 0, len(assets))

	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		name := asset.GetName()
		if err := checkSelfRuntimeAssetName(name, seen); err != nil {
			_ = os.RemoveAll(dir)

			return nil, "", err
		}

		seen[name] = struct{}{}

		path := filepath.Join(dir, name)
		if err := p.downloadSelfRuntimeAssetSnapshot(ctx, client, owner, repo, asset, path); err != nil {
			_ = os.RemoveAll(dir)

			return nil, "", err
		}

		snapshots = append(snapshots, selfRuntimeAssetSnapshot{name: name, path: path})
	}

	return snapshots, dir, nil
}

func (p *Provider) downloadSelfRuntimeAssetSnapshot(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	asset *gogithub.ReleaseAsset,
	path string,
) error {
	body, _, err := client.Repositories.DownloadReleaseAsset(ctx, owner, repo, asset.GetID(), p.httpClient())
	if err != nil {
		return fmt.Errorf("download self-runtime asset %q for snapshot: %w", asset.GetName(), classifyGitHubError(err))
	}
	defer func() { _ = body.Close() }()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path is under our private temporary directory.
	if err != nil {
		return fmt.Errorf("create snapshot for self-runtime asset %q: %w", asset.GetName(), err)
	}

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), body)
	closeErr := file.Close()

	if copyErr != nil {
		return fmt.Errorf("snapshot self-runtime asset %q: %w", asset.GetName(), copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close snapshot for self-runtime asset %q: %w", asset.GetName(), closeErr)
	}

	if written != int64(asset.GetSize()) {
		return fmt.Errorf("snapshot self-runtime asset %q read %d bytes, want %d: %w", asset.GetName(), written, asset.GetSize(), errs.ErrValidation)
	}

	if want := asset.GetDigest(); want != "" {
		got := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if got != want {
			return fmt.Errorf("snapshot self-runtime asset %q digest is %s, want %s: %w", asset.GetName(), got, want, errs.ErrValidation)
		}
	}

	return nil
}

// restoreSelfRuntimeAssets is compensation for a final post-checksum failure.
// It clears the uncommitted set, then reuses the verified release replacement
// transaction to restore every prior body and exact name.
func (p *Provider) restoreSelfRuntimeAssets(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	releaseID int64,
	snapshots []selfRuntimeAssetSnapshot,
) error {
	current, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list assets before restore: %w", err)
	}

	var restoreErr error

	for _, asset := range current {
		if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, asset.GetID()); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("delete uncommitted asset %q: %w", asset.GetName(), classifyGitHubError(err)))
		}
	}

	ordered := append([]selfRuntimeAssetSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return releaseAssetPriority(ordered[i].name) < releaseAssetPriority(ordered[j].name)
	})

	for _, snapshot := range ordered {
		if err := p.uploadAsset(ctx, client, owner, repo, releaseID, snapshot.path); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore asset %q: %w", snapshot.name, err))
		}
	}

	desired := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		desired = append(desired, snapshot.path)
	}

	if err := verifyExactReleaseAssets(ctx, client, owner, repo, releaseID, desired); err != nil {
		restoreErr = errors.Join(restoreErr, err)
	}

	return restoreErr
}

func validateSelfRuntimePublication(repository, sourceRef, targetSHA string, spec provider.ReleaseSpec) error {
	if repository != domainrelease.SelfRuntimeRepository {
		return fmt.Errorf("self-runtime publication requires repository %q: %w", domainrelease.SelfRuntimeRepository, errs.ErrValidation)
	}

	if spec.Tag != domainrelease.SelfRuntimeChannelTag {
		return fmt.Errorf("self-runtime publication requires channel tag %q: %w", domainrelease.SelfRuntimeChannelTag, errs.ErrValidation)
	}

	if !domainrelease.SelfRuntimeSourceRefAllowed(sourceRef) {
		return fmt.Errorf("self-runtime publication source ref is not trusted: %q: %w", sourceRef, errs.ErrValidation)
	}

	if !domaingit.ValidCommitSHA(targetSHA) {
		return fmt.Errorf("self-runtime publication target is not a full commit SHA: %w", errs.ErrValidation)
	}

	if spec.Draft || !spec.Prerelease || spec.MakeLatest != provider.MakeLatestFalse {
		return fmt.Errorf("self-runtime publication must be non-draft, prerelease=true, and make_latest=false: %w", errs.ErrValidation)
	}

	if err := validateSelfRuntimeAssets(spec.Assets); err != nil {
		return err
	}

	return nil
}

func githubResponseNotFound(resp *gogithub.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusNotFound
}

func requireFreshGitHubSourceRef(ctx context.Context, client *gogithub.Client, owner, repo, sourceRef, targetSHA string) error {
	ref, _, err := client.Git.GetRef(ctx, owner, repo, strings.TrimPrefix(sourceRef, "refs/"))
	if err != nil {
		return fmt.Errorf("resolve publication source ref %q: %w", sourceRef, classifyGitHubError(err))
	}

	object := ref.GetObject()
	if object == nil || object.GetType() != "commit" || object.GetSHA() != targetSHA {
		return fmt.Errorf("publication source ref %q no longer resolves to target %s: %w", sourceRef, targetSHA, errs.ErrValidation)
	}

	return nil
}

func moveGitHubTag(ctx context.Context, client *gogithub.Client, owner, repo, tag, targetSHA string, beforeMutation func() error) error {
	_, resp, err := client.Git.GetRef(ctx, owner, repo, "tags/"+tag)
	if err != nil {
		if !githubResponseNotFound(resp) {
			return fmt.Errorf("look up self-runtime tag %q: %w", tag, classifyGitHubError(err))
		}

		if err := beforeMutation(); err != nil {
			return err
		}

		if _, _, err := client.Git.CreateRef(ctx, owner, repo, gogithub.CreateRef{Ref: "refs/tags/" + tag, SHA: targetSHA}); err != nil {
			return fmt.Errorf("create self-runtime tag %q: %w", tag, classifyGitHubError(err))
		}

		return nil
	}

	if err := beforeMutation(); err != nil {
		return err
	}

	if _, _, err := client.Git.UpdateRef(ctx, owner, repo, "tags/"+tag, gogithub.UpdateRef{SHA: targetSHA, Force: gogithub.Ptr(true)}); err != nil {
		return fmt.Errorf("move self-runtime tag %q: %w", tag, classifyGitHubError(err))
	}

	return nil
}

func validateSelfRuntimeAssets(desired []string) error {
	names := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		name := filepath.Base(file)
		if name == "." || name == "" {
			return fmt.Errorf("self-runtime release asset has an empty basename: %w", errs.ErrValidation)
		}

		if _, duplicate := names[name]; duplicate {
			return fmt.Errorf("duplicate self-runtime release asset basename %q: %w", name, errs.ErrValidation)
		}

		names[name] = struct{}{}
	}

	for _, required := range []string{"checksums.txt.bundle", "checksums.txt"} {
		if _, ok := names[required]; !ok {
			return fmt.Errorf("self-runtime publication requires %s: %w", required, errs.ErrValidation)
		}
	}

	return nil
}

// reconcileSelfRuntimeAssets performs the rolling channel's intentionally
// simple in-place update. Stale assets and payloads mutate first, the signature
// bundle next, and checksums.txt last as the signed commit point. A partial
// update can reduce availability, but cannot validate as the new release.
//
//nolint:cyclop // direct replacement keeps every fail-closed mutation gate visible.
func (p *Provider) reconcileSelfRuntimeAssets(
	ctx context.Context,
	client *gogithub.Client,
	owner, repo string,
	releaseID int64,
	desired []string,
	beforeMutation func() error,
) error {
	existingAssets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("list self-runtime assets: %w", err)
	}

	desiredNames := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		desiredNames[filepath.Base(file)] = struct{}{}
	}

	existingByName := make(map[string]*gogithub.ReleaseAsset, len(existingAssets))
	for _, asset := range existingAssets {
		name := asset.GetName()
		if _, duplicate := existingByName[name]; duplicate {
			return fmt.Errorf("self-runtime release contains duplicate asset %q: %w", name, errs.ErrValidation)
		}

		existingByName[name] = asset
	}

	for _, asset := range existingAssets {
		if _, keep := desiredNames[asset.GetName()]; keep {
			continue
		}

		if err := beforeMutation(); err != nil {
			return err
		}

		if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, asset.GetID()); err != nil {
			return fmt.Errorf("delete stale self-runtime asset %q: %w", asset.GetName(), classifyGitHubError(err))
		}
	}

	ordered := append([]string(nil), desired...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return releaseAssetPriority(filepath.Base(ordered[i])) < releaseAssetPriority(filepath.Base(ordered[j]))
	})

	for _, file := range ordered {
		name := filepath.Base(file)

		opened, size, digest, err := openReleaseAsset(file)
		if err != nil {
			return err
		}

		if existing := existingByName[name]; existing != nil {
			if err := beforeMutation(); err != nil {
				_ = opened.Close()

				return err
			}

			if _, err := client.Repositories.DeleteReleaseAsset(ctx, owner, repo, existing.GetID()); err != nil {
				_ = opened.Close()

				return fmt.Errorf("delete existing self-runtime asset %q: %w", name, classifyGitHubError(err))
			}
		}

		// For checksums.txt this freshness read is the signed-checksum commit
		// gate and occurs immediately before the upload mutation.
		if err := beforeMutation(); err != nil {
			_ = opened.Close()

			return err
		}

		_, uploadErr := uploadVerifiedReleaseAsset(ctx, client, owner, repo, releaseID, name, opened, size, digest, beforeMutation)
		_ = opened.Close()

		if uploadErr != nil {
			return uploadErr
		}
	}

	return nil
}

func verifyExactReleaseAssets(ctx context.Context, client *gogithub.Client, owner, repo string, releaseID int64, desired []string) error {
	assets, err := listAllReleaseAssets(ctx, client, owner, repo, releaseID)
	if err != nil {
		return fmt.Errorf("verify self-runtime release assets: %w", err)
	}

	want := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		name := filepath.Base(file)
		if _, duplicate := want[name]; duplicate {
			return fmt.Errorf("duplicate self-runtime release asset basename %q: %w", name, errs.ErrValidation)
		}

		want[name] = struct{}{}
	}

	if len(assets) != len(want) {
		return fmt.Errorf("self-runtime release has %d assets, want exactly %d: %w", len(assets), len(want), errs.ErrValidation)
	}

	for _, asset := range assets {
		if _, ok := want[asset.GetName()]; !ok {
			return fmt.Errorf("self-runtime release contains unexpected asset %q: %w", asset.GetName(), errs.ErrValidation)
		}
	}

	return nil
}

func resolveGitHubTag(ctx context.Context, client *gogithub.Client, owner, repo, tag string) (string, error) {
	ref, _, err := client.Git.GetRef(ctx, owner, repo, "tags/"+tag)
	if err != nil {
		return "", fmt.Errorf("resolve self-runtime tag %q: %w", tag, classifyGitHubError(err))
	}

	object := ref.GetObject()
	if object == nil || object.GetType() != "commit" || object.GetSHA() == "" {
		return "", fmt.Errorf("resolve self-runtime tag %q: expected a lightweight tag targeting a commit: %w", tag, errs.ErrValidation)
	}

	return object.GetSHA(), nil
}

// checkSelfRuntimeAssetName rejects an asset name that cannot safely become a
// file inside the snapshot directory.
//
// ".." is the case worth naming: it is a single path component, so
// filepath.Base(name) == name accepts it and the separator check never fires.
// Joined onto the snapshot directory it names the PARENT — the one place this
// guard exists to keep the download out of. It used to reach the download and
// fail there as "open /tmp: file exists", which is neither a refusal nor a
// diagnostic anyone can act on.
func checkSelfRuntimeAssetName(name string, seen map[string]struct{}) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("self-runtime release contains unsafe asset name %q: %w", name, errs.ErrValidation)
	}

	if _, duplicate := seen[name]; duplicate {
		return fmt.Errorf("self-runtime release contains duplicate asset %q: %w", name, errs.ErrValidation)
	}

	return nil
}
