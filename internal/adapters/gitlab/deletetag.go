// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DeleteTag removes a container tag from the GitLab Container Registry via the
// project registry API, keeping the manifest.
//
// DELETE /projects/:id/registry/repositories/:repository_id/tags/:tag_name is
// tag-scoped and synchronous, which is exactly what the shared-digest promotion
// model needs: staging and final tags point at one manifest, so a manifest-level
// delete would destroy the promoted release image. (GitLab's *repository*
// delete is asynchronous and unsafe for the same reason — it is never used
// here.)
//
// Implements provider.TagDeleter and imageledger.TagDeleter.
func (p *Provider) DeleteTag(ctx context.Context, ref string) error {
	parsed, err := container.ParseTaggedRef(ref)
	if err != nil {
		return err
	}

	apiBase, headers := p.apiContext()

	project, repositoryID, err := p.resolveRegistryRepository(ctx, apiBase, headers, parsed.Path)
	if err != nil {
		return fmt.Errorf("gitlab delete container tag %q: %w", ref, err)
	}

	// Nothing to untag: the repository does not exist on this instance.
	if repositoryID == 0 {
		return nil
	}

	endpoint := projectEndpoint(apiBase, project) +
		"/registry/repositories/" + strconv.FormatInt(repositoryID, 10) +
		"/tags/" + url.PathEscape(parsed.Tag)

	if err := deleteJSON(ctx, p.HTTPClient, endpoint, headers); err != nil {
		// A tag that is already gone is the outcome the caller wanted.
		if errors.Is(err, errs.ErrMissingInput) {
			return nil
		}

		return fmt.Errorf("gitlab delete container tag %q: %w", ref, err)
	}

	return nil
}

// registryRepository is the subset of GitLab's registry-repository object this
// adapter reads: the numeric id the tag endpoint is keyed on, and the path used
// to confirm the match.
type registryRepository struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// resolveRegistryRepository maps a repository path from the ref onto the
// project that holds it and the registry repository's numeric id, returning a
// zero id when no such repository exists on this instance.
//
// Two lookups are needed because GitLab keys the tag endpoint on a repository
// id, and offers no lookup by path. Registry repositories may also nest below
// their project (group/project/image), so the project is found by trying the
// ref's path and then successively shorter prefixes, longest first — which
// resolves in one call for the common case where the image path IS the project
// path.
//
// Resolution is from the ref, never from $CI_PROJECT_ID: a ledger can name a
// repository other than the running project, and deleting from the ambient
// project because the ref was ignored is precisely the failure worth designing
// out. The result is then verified — only a registry repository whose own path
// equals the ref's is accepted — so a prefix match can never delete a tag from
// a neighbouring image. The ref's registry host is not used to select the
// server (it addresses the configured instance, as on Forgejo); callers that
// need to pin the repository have `--expected-image-repository`.
func (p *Provider) resolveRegistryRepository(
	ctx context.Context, apiBase string, headers map[string]string, path string,
) (string, int64, error) {
	segments := strings.Split(path, "/")

	// Longest first, down to the two-segment minimum a project path has.
	const ownerAndName = 2
	for size := len(segments); size >= ownerAndName; size-- {
		project := strings.Join(segments[:size], "/")

		repositories, err := p.listRegistryRepositories(ctx, apiBase, headers, project)
		if err != nil {
			return "", 0, err
		}

		for _, repository := range repositories {
			if repository.Path == path {
				return project, repository.ID, nil
			}
		}
	}

	return "", 0, nil
}

// listRegistryRepositories returns a project's registry repositories, or none
// when the project does not exist — an absent project is a "no such repository"
// answer while probing prefixes, not a failure.
func (p *Provider) listRegistryRepositories(
	ctx context.Context, apiBase string, headers map[string]string, project string,
) ([]registryRepository, error) {
	endpoint := projectEndpoint(apiBase, project) + "/registry/repositories?per_page=100"

	body, err := getJSON(ctx, p.HTTPClient, endpoint, headers)
	if err != nil {
		if errors.Is(err, errs.ErrMissingInput) {
			return nil, nil
		}

		return nil, fmt.Errorf("list registry repositories for %s: %w", project, err)
	}

	var repositories []registryRepository
	if err := json.Unmarshal(body, &repositories); err != nil {
		return nil, fmt.Errorf("decode registry repositories for %s: %w: %w", project, err, errs.ErrMalformedInput)
	}

	return repositories, nil
}
