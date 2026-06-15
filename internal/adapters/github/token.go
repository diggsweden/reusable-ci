// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// ValidateToken makes a single GET /repos/{repo} call with the given
// token. Format / prefix checks happen in the use-case layer; this
// method only reports whether the API accepts the token for that repo.
func (p *Provider) ValidateToken(ctx context.Context, token, repo string) error {
	if token == "" {
		return fmt.Errorf("token is empty: %w", errs.ErrPermissionDenied)
	}

	if repo == "" {
		return fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = p.envFunc()("GITHUB_API_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	url := strings.TrimRight(apiBase, "/") + "/repos/" + repo

	_, err := getJSON(ctx, p.HTTPClient, url, map[string]string{
		"Accept":               acceptJSONHeader,    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"X-GitHub-Api-Version": apiVersionHeader,    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Authorization":        bearerHeader(token), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		return fmt.Errorf("github token validation: %w: %w", err, errs.ErrPermissionDenied)
	}

	return nil
}

// ValidateBotPermissions runs three best-effort probes against the
// configured bot token (read from $GITHUB_TOKEN via the env getter).
// Returns ok=true / ok=false per probe rather than failing fast — the
// use case decides which probe is fatal vs warn-only.
//
// The probes are independent HTTP GETs, so they fan out via goroutines
// rather than serialising 3 sequential RTTs. http.Client connection
// pooling (and HTTP/2 multiplexing where the server supports it) keeps
// the concurrent cost ~one round trip's worth, vs ~three sequentially.
func (p *Provider) ValidateBotPermissions(ctx context.Context, repo string) (*provider.BotPermissions, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}

	get := p.envFunc()
	token := get("GITHUB_TOKEN")

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("GITHUB_API_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	headers := map[string]string{
		"Accept":               acceptJSONHeader,
		"X-GitHub-Api-Version": apiVersionHeader,
		"Authorization":        bearerHeader(token),
	}
	probe := func(path string) bool {
		_, err := getJSON(ctx, p.HTTPClient, strings.TrimRight(apiBase, "/")+path, headers)

		return err == nil
	}

	var (
		bp provider.BotPermissions
		wg sync.WaitGroup
	)

	wg.Add(3)

	go func() { defer wg.Done(); bp.UserAccessible = probe("/user") }()
	go func() { defer wg.Done(); bp.RepoAccessible = probe("/repos/" + repo) }()
	go func() { defer wg.Done(); bp.BranchesAccessible = probe("/repos/" + repo + "/branches") }()

	wg.Wait()

	return &bp, nil
}
