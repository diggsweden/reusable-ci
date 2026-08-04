// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// FetchRepoMetadata calls `GET /api/v4/projects/{repo}` on the GitLab
// REST API. The repo path is URL-encoded so group/sub/project paths
// work. Authentication uses the PRIVATE-TOKEN header populated from
// $GITLAB_TOKEN, falling back to $CI_JOB_TOKEN (the per-pipeline
// short-lived token GitLab CI exports automatically).
//
// On any failure, returns a non-nil error; on missing metadata fields,
// returns a *RepoMetadata with empty fields. Caller treats both the
// same: best-effort OCI labels.
func (p *Provider) FetchRepoMetadata(ctx context.Context, repo string) (*provider.RepoMetadata, error) {
	if repo == "" {
		return &provider.RepoMetadata{}, nil
	}

	apiBase, headers := p.apiContext()

	body, err := getJSON(ctx, p.HTTPClient, projectEndpoint(apiBase, repo), headers)
	if err != nil {
		return nil, fmt.Errorf("gitlab fetch project metadata: %w", err)
	}

	type glLicense struct {
		Key      string `json:"key"`
		Nickname string `json:"nickname"`
	}

	type glProject struct {
		Description string    `json:"description"`
		WebURL      string    `json:"web_url"`
		License     glLicense `json:"license"`
	}

	var pr glProject
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("gitlab decode project response: %w", err)
	}
	// GitLab returns license.key (lowercase, e.g. "apache-2.0"). The
	// SPDX form is the upper-cased version, e.g. "Apache-2.0". The Key
	// is good enough for our OCI label use; uppercase only when we know
	// the canonical SPDX form.
	return &provider.RepoMetadata{
		Description: pr.Description,
		HTMLURL:     pr.WebURL,
		LicenseSPDX: pr.License.Key,
	}, nil
}
