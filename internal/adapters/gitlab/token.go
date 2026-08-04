// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ValidateToken makes a single GET /api/v4/projects/{enc-path} call
// with the given token (sent as PRIVATE-TOKEN). Format checks
// (glpat_*) are caller-side. Returns nil on 2xx, an error otherwise.
func (p *Provider) ValidateToken(ctx context.Context, token, repo string) error {
	if token == "" {
		return fmt.Errorf("token is empty: %w", errs.ErrPermissionDenied)
	}

	if repo == "" {
		return fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}

	apiBase, _ := p.apiContext()
	endpoint := projectEndpoint(apiBase, repo)

	_, err := getJSON(ctx, p.HTTPClient, endpoint, map[string]string{
		"PRIVATE-TOKEN": token, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		return fmt.Errorf("gitlab token validation: %w", err)
	}

	return nil
}

// ValidateBotPermissions probes the GitLab API with the configured bot
// token. Returns ok=true/false per probe; the use case decides
// severity.
//
// Mapping to GitHub's three probes:
//
//	UserAccessible     → GET /api/v4/user
//	RepoAccessible     → GET /api/v4/projects/{enc-path}
//	BranchesAccessible → GET /api/v4/projects/{enc-path}/repository/branches
//
// Probes fan out so the total cost is one round trip's worth rather
// than three serialised RTTs. Mirrors the github provider's
// ValidateBotPermissions shape so the port stays uniform across
// implementations.
func (p *Provider) ValidateBotPermissions(ctx context.Context, repo string) (*provider.BotPermissions, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}

	apiBase, headers := p.apiContext()
	encoded := url.PathEscape(repo)
	probe := func(path string) bool {
		_, err := getJSON(ctx, p.HTTPClient, strings.TrimRight(apiBase, "/")+path, headers)

		return err == nil
	}

	var (
		bp provider.BotPermissions
		wg sync.WaitGroup
	)

	wg.Add(3)

	go func() { defer wg.Done(); bp.UserAccessible = probe("/api/v4/user") }()
	go func() { defer wg.Done(); bp.RepoAccessible = probe("/api/v4/projects/" + encoded) }()
	go func() {
		defer wg.Done()

		bp.BranchesAccessible = probe("/api/v4/projects/" + encoded + "/repository/branches")
	}()

	wg.Wait()

	return &bp, nil
}
