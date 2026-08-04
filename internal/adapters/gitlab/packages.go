// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// gitlabTagPageSize is the per-page size for registry tag listing. GitLab caps
// per_page at 100, and staging cleanup enumerates every tag of one image, so the
// maximum keeps the round trips down.
const gitlabTagPageSize = 100

// ListContainerPackageVersions enumerates the tags of one container image
// through the GitLab registry API, satisfying provider.ContainerPackageLister.
//
// Together with DeleteTag this is what base-image staging cleanup needs — list
// the versions, decide which are stale, delete those tags — so implementing it
// is what lets that flow run on GitLab rather than Forgejo alone.
//
// owner/name are the repository path the ref carries, matched against the
// registry repository's own path exactly as DeleteTag does, so listing and
// deleting can never disagree about which image they mean.
func (p *Provider) ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error) {
	if owner == "" || name == "" {
		return nil, fmt.Errorf("gitlab list container tags: owner and name are required: %w", errs.ErrUsage)
	}

	path := owner + "/" + name
	apiBase, headers := p.apiContext()

	project, repositoryID, err := p.resolveRegistryRepository(ctx, apiBase, headers, path)
	if err != nil {
		return nil, fmt.Errorf("gitlab list container tags for %s: %w", path, err)
	}

	// No such image: an empty version list, not an error. Cleanup runs against
	// images that may never have been pushed.
	if repositoryID == 0 {
		return []string{}, nil
	}

	return p.listRegistryTags(ctx, apiBase, headers, project, repositoryID, path)
}

// listRegistryTags pages through one registry repository's tags.
func (p *Provider) listRegistryTags(
	ctx context.Context, apiBase string, headers map[string]string, project string, repositoryID int64, path string,
) ([]string, error) {
	base := projectEndpoint(apiBase, project) +
		"/registry/repositories/" + strconv.FormatInt(repositoryID, 10) + "/tags"

	versions := make([]string, 0)

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s?per_page=%d&page=%d", base, gitlabTagPageSize, page)

		body, err := getJSON(ctx, p.HTTPClient, endpoint, headers)
		if err != nil {
			// The repository disappeared between resolution and listing; that
			// is an empty answer, not a failure.
			if errors.Is(err, errs.ErrMissingInput) {
				return versions, nil
			}

			return nil, fmt.Errorf("gitlab list container tags for %s: %w", path, err)
		}

		var tags []struct {
			Name string `json:"name"`
		}

		if err := json.Unmarshal(body, &tags); err != nil {
			return nil, fmt.Errorf("gitlab decode container tags for %s: %w: %w", path, err, errs.ErrMalformedInput)
		}

		if len(tags) == 0 {
			return versions, nil
		}

		for _, tag := range tags {
			versions = append(versions, tag.Name)
		}

		// A short page is the last one; asking for another costs a round trip
		// to learn nothing.
		if len(tags) < gitlabTagPageSize {
			return versions, nil
		}
	}
}
