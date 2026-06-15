// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// DeleteTag removes a container tag from the Forgejo registry via the
// package API (Gitea SDK DeletePackage), keeping the manifest. This is
// the only SAFE delete for the shared-digest promotion model: staging
// and final tags share one manifest (promotion is a digest-preserving
// retag), so a manifest-level delete (skopeo) would destroy the promoted
// release image. The package API deletes the tag's package *version*,
// leaving the manifest reachable by the final tag.
//
// ref is a full image ref, e.g. "codeberg.org/owner/repo:staging-v1.2.3";
// owner/name/tag are parsed relative to the resolved server host.
// Implements provider.TagDeleter and imageledger.TagDeleter.
func (p *Provider) DeleteTag(ctx context.Context, ref string) error {
	owner, name, tag, err := parseContainerRef(ref)
	if err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return err
	}

	if resp, err := client.DeletePackage(owner, "container", name, tag); err != nil {
		return fmt.Errorf("forgejo delete container tag %q: %w", ref, classifyErr(resp, err))
	}

	return nil
}

// parseContainerRef splits a fully-qualified registry ref
// (<host>/<owner>/<name>:<tag>) into the owner, package name, and tag the
// package API needs. The leading registry host is the first path segment
// and is dropped — the delete call targets the configured Forgejo
// server, not the ref's host. The tag is the suffix after the final ':'
// when that ':' belongs to the final path component (not a host:port).
func parseContainerRef(ref string) (string, string, string, error) {
	// A digest-pinned ref (@sha256:…) names a manifest, not a tag — there
	// is nothing to untag. Reject before the ':'-splitting below would
	// mistake the digest for a tag.
	if strings.Contains(ref, "@") {
		return "", "", "", fmt.Errorf("container ref %q is digest-pinned, not a tag: %w", ref, errs.ErrUsage)
	}

	slash := strings.LastIndex(ref, "/")

	colon := strings.LastIndex(ref, ":")
	if colon <= slash {
		return "", "", "", fmt.Errorf("container ref %q has no tag to delete: %w", ref, errs.ErrUsage)
	}

	tag := ref[colon+1:]

	// host / owner / name... — drop the host, keep owner + (slash-joined) name.
	parts := strings.Split(ref[:colon], "/")
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("container ref %q is not <host>/<owner>/<name>:<tag>: %w", ref, errs.ErrUsage)
	}

	return parts[1], strings.Join(parts[2:], "/"), tag, nil
}
