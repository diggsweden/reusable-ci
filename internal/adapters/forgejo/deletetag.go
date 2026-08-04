// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"context"
	"fmt"
	"net/http"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// DeleteTag removes a container tag from the Forgejo registry via the
// package API (Gitea SDK DeletePackage), keeping the manifest. This is
// the only SAFE delete for the shared-digest promotion model: staging
// and final tags share one manifest (promotion is a digest-preserving
// retag), so a manifest-level delete (skopeo) would destroy the promoted
// release image. The package API deletes the tag's package *version*,
// leaving the manifest reachable by the final tag.
//
// ref is a full image ref, e.g. "codeberg.org/owner/repo:staging-v1.2.3". The
// split is container.ParseTaggedRef — shared with the other forge adapters so
// the digest-pinned refusal is stated once — and the parts are resolved against
// the configured Forgejo server rather than the ref's own host.
// Implements provider.TagDeleter and imageledger.TagDeleter.
func (p *Provider) DeleteTag(ctx context.Context, ref string) error {
	parsed, err := container.ParseTaggedRef(ref)
	if err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return err
	}

	if resp, err := client.DeletePackage(parsed.Owner(), "container", parsed.Name(), parsed.Tag); err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil
		}

		return fmt.Errorf("forgejo delete container tag %q: %w", ref, classifyErr(resp, err))
	}

	return nil
}
