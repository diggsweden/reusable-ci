// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ociregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ListContainerPackageVersions returns the tags of one repository, satisfying
// provider.ContainerPackageLister against a plain OCI distribution registry.
//
// The forge adapters answer this from a package API that knows about owners
// and package names; a registry only knows repositories, so owner and name are
// joined back into the repository path the caller's refs already use.
//
// This exists so a self-hosted or local registry can serve the same base-image
// lifecycle as a forge's package registry. A runner that keeps its base images
// in a registry beside it, rather than pushing every content-addressed rebuild
// to a remote package host, is the case it was added for.
func (a *Adapter) ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error) {
	return a.listRepositoryTags(ctx, joinRepository(owner, name))
}

// joinRepository rebuilds a repository path from an owner/name pair, tolerating
// an empty half rather than emitting the trailing slash that would strip the
// registry host and silently resolve against Docker Hub.
func joinRepository(owner, name string) string {
	parts := make([]string, 0, 2)

	for _, part := range []string{owner, name} {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	return strings.Join(parts, "/")
}

// listRepositoryTags lists one repository's tags by its full path.
func (a *Adapter) listRepositoryTags(ctx context.Context, repository string) ([]string, error) {
	tags, err := crane.ListTags(repository, a.craneOpts(ctx, repository)...)
	if err != nil {
		return nil, fmt.Errorf("list tags for %s: %w: %w", repository, err, classifyRegistryError(err))
	}

	return tags, nil
}

// DeleteTag removes one tag, satisfying provider.TagDeleter against a plain OCI
// distribution registry.
//
// The distribution spec has no delete-tag operation: DELETE takes a manifest
// digest and removes the manifest, taking every tag that shares it. A forge
// package API deletes the version and leaves siblings alone, so the same call
// is safe there and destructive here.
//
// The gap is closed by refusing rather than by hoping: the tag's digest is
// resolved, the repository's tags are checked for another pointing at it, and
// a shared manifest aborts the delete. Base-image retention deletes a
// content-addressed tag that by construction has no siblings, so the check
// costs one listing and turns "usually fine" into "verified before the write".
func (a *Adapter) DeleteTag(ctx context.Context, ref string) error {
	parsed, err := container.ParseTaggedRef(ref)
	if err != nil {
		return err
	}

	digest, err := a.ResolveDigest(ctx, ref)
	if err != nil {
		// Already gone is the desired state, and retention runs repeatedly.
		if errors.Is(err, errs.ErrMissingInput) {
			return nil
		}

		return err
	}

	// Host + Path, not Owner + Name: those two split Path for a forge package
	// API keyed on an owner, and drop the registry host a repository path needs.
	repository := parsed.Host + "/" + parsed.Path

	if err := a.refuseSharedManifest(ctx, repository, parsed.Tag, digest); err != nil {
		return err
	}

	if err := crane.Delete(ref, a.craneOpts(ctx, ref)...); err != nil {
		return fmt.Errorf("delete tag %s: %w: %w", ref, err, classifyRegistryError(err))
	}

	return nil
}

// refuseSharedManifest fails when a tag other than tag resolves to digest.
//
// Deleting through this adapter removes the manifest, so a second tag on the
// same digest would disappear with it. That is exactly the accident the forge
// path is careful to avoid -- staging and final tags share a manifest, which is
// why base-image cleanup deletes package versions and never digests.
func (a *Adapter) refuseSharedManifest(ctx context.Context, repository, tag, digest string) error {
	tags, err := a.listRepositoryTags(ctx, repository)
	if err != nil {
		return err
	}

	for _, other := range tags {
		if other == tag {
			continue
		}

		otherDigest, err := a.ResolveDigest(ctx, repository+":"+other)
		if err != nil {
			// A tag that vanished mid-listing cannot be sharing anything.
			if errors.Is(err, errs.ErrMissingInput) {
				continue
			}

			return err
		}

		if otherDigest == digest {
			return fmt.Errorf(
				"refusing to delete %s:%s: tag %q serves the same manifest (%s) and deleting by digest "+
					"would remove both; delete through the forge package API instead: %w",
				repository, tag, other, digest, errs.ErrValidation)
		}
	}

	return nil
}
