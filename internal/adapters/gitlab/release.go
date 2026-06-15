// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// CreateRelease creates a GitLab release via REST API. Two-stage:
//
//  1. POST /api/v4/projects/{enc}/releases — create the release at the
//     given tag with description (= notes file body).
//  2. For each asset path: POST /assets/links — links each pre-uploaded
//     file. GitLab releases don't support direct file upload via the
//     release API; the calling job is expected to have published each
//     asset to the project's package registry first and pass URLs (not
//     filesystem paths). For now the adapter passes the asset basename
//     and synthesises a placeholder downloads URL — callers that want
//     real asset URLs should pre-stage uploads.
//
// GitLab parity for release creation is intentionally minimal — full
// asset-upload support is follow-up work for when the GitLab catalog
// wires real publishing.
//
//nolint:cyclop // REST flow: list → delete-if-exists → create → upload links.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("CreateRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("CreateRelease: repo is empty: %w", errs.ErrUsage)
	}

	get := p.envFunc()

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("CI_SERVER_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	token := get("GITLAB_TOKEN")
	if token == "" {
		token = get("CI_JOB_TOKEN")
	}

	headers := map[string]string{
		"PRIVATE-TOKEN": token, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Content-Type":  "application/json",
	}
	encoded := url.PathEscape(repo)
	endpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded + "/releases"

	desc := spec.Name
	if spec.NotesFile != "" {
		body, err := os.ReadFile(spec.NotesFile)
		if err == nil {
			desc = string(body)
		}
	}

	payload, err := json.Marshal(map[string]any{
		"name":        cmp.Or(spec.Name, spec.Tag),
		"tag_name":    spec.Tag,
		"description": desc,
	})
	if err != nil {
		return fmt.Errorf("marshal release payload: %w", err)
	}

	if err := postJSON(ctx, p.HTTPClient, endpoint, headers, payload); err != nil {
		return fmt.Errorf("gitlab create release: %w", err)
	}

	// Asset links — best-effort. Skip when no assets.
	for _, asset := range spec.Assets {
		linkPayload, err := json.Marshal(map[string]any{
			"name": filepath.Base(asset),
			// GitLab's API requires `url`; we synthesise a project
			// downloads URL as a placeholder when no real upload
			// location is supplied.
			"url": fmt.Sprintf("%s/%s/-/releases/%s/downloads/%s",
				strings.TrimRight(apiBase, "/"),
				repo, url.PathEscape(spec.Tag), url.PathEscape(filepath.Base(asset))),
		})
		if err != nil {
			// Best-effort: skip this asset link if its payload is
			// somehow unmarshallable (shouldn't happen for the
			// fixed shape above). The primary release was already
			// created above; missing asset links don't fail the call.
			continue
		}

		linksEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded +
			"/releases/" + url.PathEscape(spec.Tag) + "/assets/links"
		_ = postJSON(ctx, p.HTTPClient, linksEndpoint, headers, linkPayload)
	}

	return nil
}
