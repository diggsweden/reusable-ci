// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// FetchRepoMetadata calls `GET /repos/{repo}` on the GitHub REST API
// and returns the description / html_url / SPDX license id. The token
// is optional — public repos work unauthenticated (with reduced rate
// limits).
//
// Returns a non-nil *RepoMetadata even on success of an empty response;
// returns an error only on transport / parse failures. Callers that
// need OCI labels treat the missing-fields case as "leave the label
// empty" rather than failing the release.
func (p *Provider) FetchRepoMetadata(ctx context.Context, repo string) (*provider.RepoMetadata, error) {
	if repo == "" {
		return &provider.RepoMetadata{}, nil
	}

	get := p.envFunc()

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("GITHUB_API_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	url := strings.TrimRight(apiBase, "/") + "/repos/" + repo

	body, err := getJSON(ctx, p.HTTPClient, url, map[string]string{
		"Accept":               acceptJSONHeader,                  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"X-GitHub-Api-Version": apiVersionHeader,                  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Authorization":        bearerHeader(get("GITHUB_TOKEN")), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		return nil, fmt.Errorf("github fetch repo metadata: %w", err)
	}

	type ghLicense struct {
		SPDXID string `json:"spdx_id"`
	}

	type ghRepo struct {
		Description string    `json:"description"`
		HTMLURL     string    `json:"html_url"`
		License     ghLicense `json:"license"`
	}

	var r ghRepo //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("github decode repo response: %w", err)
	}

	// ObjectFormat is intentionally left empty: the GitHub REST API exposes
	// no object_format (GitHub repos are sha1), and an empty value tells the
	// caller to default to sha1.
	return &provider.RepoMetadata{
		Description: r.Description,
		HTMLURL:     r.HTMLURL,
		LicenseSPDX: r.License.SPDXID,
	}, nil
}
