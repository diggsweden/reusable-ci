// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"context"
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// FetchRepoMetadata calls GET /api/v1/repos/{owner}/{repo} and maps the
// description + web URL into RepoMetadata for OCI labels. An empty repo
// (or an unparseable one) returns empty metadata best-effort, matching
// the gitlab adapter; a network/API failure returns an error.
func (p *Provider) FetchRepoMetadata(ctx context.Context, repo string) (*provider.RepoMetadata, error) {
	if repo == "" {
		return &provider.RepoMetadata{}, nil
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return &provider.RepoMetadata{}, nil //nolint:nilerr // best-effort OCI labels: an unparseable repo yields empty metadata, not a hard failure.
	}

	client, err := p.client(ctx)
	if err != nil {
		return nil, err
	}

	repoInfo, resp, err := client.GetRepo(owner, name)
	if err != nil {
		return nil, fmt.Errorf("forgejo fetch repo metadata: %w", classifyErr(resp, err))
	}

	return &provider.RepoMetadata{
		Description:  repoInfo.Description,
		HTMLURL:      repoInfo.HTMLURL,
		ObjectFormat: repoInfo.ObjectFormatName, // "sha1"/"sha256" on Gitea/Forgejo >= 1.22; "" on older servers.
	}, nil
}
