// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// UploadSARIF posts a SARIF report to the GitHub Code Scanning API.
// The SARIF body is gzip-then-base64 encoded as the API requires, then
// wrapped in the JSON payload shape {commit_sha, ref, sarif}. The analysis
// category lives inside the SARIF (each run's automationDetails.id), so the
// payload carries no separate tool_name/category.
//
// Skip semantics (return nil): caller-side — empty Token or empty
// SARIF body. This method always attempts the POST; failures propagate.
func (p *Provider) UploadSARIF(ctx context.Context, up provider.SARIFUpload) error {
	if up.Token == "" {
		return fmt.Errorf("github upload sarif: token is empty: %w", errs.ErrPermissionDenied)
	}

	if up.Repository == "" {
		return fmt.Errorf("github upload sarif: repository is empty: %w", errs.ErrUsage)
	}

	encoded, err := gzipBase64(up.SARIF)
	if err != nil {
		return fmt.Errorf("github upload sarif: encode: %w", err)
	}

	payload := map[string]string{
		"commit_sha": up.SHA,
		"ref":        up.Ref,
		"sarif":      encoded,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("github upload sarif: marshal: %w", err)
	}

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = p.envFunc()("GITHUB_API_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	url := strings.TrimRight(apiBase, "/") + "/repos/" + up.Repository + "/code-scanning/sarifs"

	return postJSON(ctx, p.HTTPClient, url, map[string]string{
		"Authorization": "token " + up.Token, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Accept":        acceptJSONHeader,    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Content-Type":  "application/json",
	}, body)
}
