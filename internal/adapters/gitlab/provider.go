// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package gitlab implements provider.Provider for GitLab CI.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Provider satisfies provider.Provider for the GitLab CI runtime.
type Provider struct {
	// Env holds the env-var snapshot used for resolution. Empty values
	// fall back to os.Getenv at the time ResolveContext is called.
	// Tests substitute a fixed map.
	Env func(string) string

	// HTTPClient overrides http.DefaultClient. Tests inject an
	// httptest-backed client; production leaves it nil.
	HTTPClient *http.Client

	// APIBaseOverride overrides the default https://gitlab.com.
	// Production reads CI_SERVER_URL via Env. Tests set this directly.
	APIBaseOverride string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider {
	return &Provider{Env: os.Getenv}
}

// Name reports the platform identifier.
func (p *Provider) Name() provider.Platform { return provider.PlatformGitLab }

// ResolveContext reads the canonical GitLab CI env vars and returns a
// typed EventContext. No network, no I/O.
//
// Mapping:
//
//	CI_COMMIT_REF_NAME       → RefName
//	CI_COMMIT_TAG (presence) → RefType=Tag, else RefType=Branch
//	CI_PIPELINE_SOURCE=merge_request_event → RefType=PR
//	CI_COMMIT_SHA            → SHA
//	CI_COMMIT_SHORT_SHA      → ShortSHA  (CI_COMMIT_SHA[:7] fallback)
//	CI_COMMIT_BRANCH || CI_MERGE_REQUEST_SOURCE_BRANCH_NAME → Branch
//	CI_MERGE_REQUEST_IID     → PRNumber
//	CI_PIPELINE_SOURCE       → EventName
//	CI_PROJECT_PATH          → Repo
//	CI_PROJECT_URL           → RepoURL
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.Env
	if get == nil {
		get = os.Getenv
	}

	sha := get("CI_COMMIT_SHA")
	short := get("CI_COMMIT_SHORT_SHA")
	if short == "" && len(sha) > 7 {
		short = sha[:7]
	}

	refType := classifyRefType(get)

	branch := get("CI_COMMIT_BRANCH")
	if branch == "" {
		branch = get("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME")
	}

	prNumber := get("CI_MERGE_REQUEST_IID")

	return &provider.EventContext{
		Platform:  provider.PlatformGitLab,
		RefName:   get("CI_COMMIT_REF_NAME"),
		RefType:   refType,
		SHA:       sha,
		ShortSHA:  short,
		Branch:    branch,
		PRNumber:  prNumber,
		EventName: get("CI_PIPELINE_SOURCE"),
		Repo:      get("CI_PROJECT_PATH"),
		RepoURL:   get("CI_PROJECT_URL"),
	}, nil
}

// classifyRefType resolves the ref type from a GitLab CI env snapshot.
// Order of precedence:
//
//  1. CI_PIPELINE_SOURCE=merge_request_event → PR (overrides everything)
//  2. CI_COMMIT_TAG present                  → Tag
//  3. CI_COMMIT_BRANCH present               → Branch
//  4. CI_COMMIT_REF_NAME present             → Branch (fallback when
//     CI_COMMIT_BRANCH isn't
//     populated, e.g. detached
//     HEAD pipelines)
//  5. otherwise                              → Other
func classifyRefType(get func(string) string) provider.RefType {
	if get("CI_PIPELINE_SOURCE") == "merge_request_event" {
		return provider.RefTypePR
	}
	if get("CI_COMMIT_TAG") != "" {
		return provider.RefTypeTag
	}
	if get("CI_COMMIT_BRANCH") != "" {
		return provider.RefTypeBranch
	}
	if get("CI_COMMIT_REF_NAME") != "" {
		return provider.RefTypeBranch
	}
	return provider.RefTypeOther
}

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
	get := p.envFunc()

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("CI_SERVER_URL")
	}
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	encoded := url.PathEscape(repo)
	endpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded

	token := get("GITLAB_TOKEN")
	if token == "" {
		token = get("CI_JOB_TOKEN")
	}

	body, err := getJSON(ctx, p.HTTPClient, endpoint, map[string]string{
		"PRIVATE-TOKEN": token,
	})
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
	spdx := pr.License.Key
	return &provider.RepoMetadata{
		Description: pr.Description,
		HTMLURL:     pr.WebURL,
		LicenseSPDX: spdx,
	}, nil
}

// ValidateToken makes a single GET /api/v4/projects/{enc-path} call with
// the given token (sent as PRIVATE-TOKEN). Format checks (glpat_*) are
// caller-side. Returns nil on 2xx, an error otherwise.
func (p *Provider) ValidateToken(ctx context.Context, token, repo string) error {
	if token == "" {
		return fmt.Errorf("token is empty: %w", errs.ErrPermissionDenied)
	}
	if repo == "" {
		return fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}
	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = p.envFunc()("CI_SERVER_URL")
	}
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	endpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + url.PathEscape(repo)
	_, err := getJSON(ctx, p.HTTPClient, endpoint, map[string]string{
		"PRIVATE-TOKEN": token,
	})
	if err != nil {
		return fmt.Errorf("gitlab token validation: %w", err)
	}
	return nil
}

// ValidateBotPermissions probes the GitLab API with the configured bot
// token. Returns ok=true/false per probe; use case decides severity.
//
// Mapping to GitHub's three probes:
//
//	UserAccessible     → GET /api/v4/user
//	RepoAccessible     → GET /api/v4/projects/{enc-path}
//	BranchesAccessible → GET /api/v4/projects/{enc-path}/repository/branches
func (p *Provider) ValidateBotPermissions(ctx context.Context, repo string) (*provider.BotPermissions, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is empty: %w", errs.ErrUsage)
	}
	get := p.envFunc()
	token := get("GITLAB_TOKEN")
	if token == "" {
		token = get("CI_JOB_TOKEN")
	}

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("CI_SERVER_URL")
	}
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	headers := map[string]string{"PRIVATE-TOKEN": token}
	encoded := url.PathEscape(repo)
	probe := func(path string) bool {
		_, err := getJSON(ctx, p.HTTPClient, strings.TrimRight(apiBase, "/")+path, headers)
		return err == nil
	}
	// Three independent best-effort HTTP GETs — fan out so the cost is
	// one round trip's worth rather than three serialised RTTs. Mirrors
	// the github provider's ValidateBotPermissions shape so the port
	// stays uniform across implementations.
	var bp provider.BotPermissions
	var wg sync.WaitGroup
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

// CreateRelease creates a GitLab release via REST API. Two-stage:
//
//  1. POST /api/v4/projects/{enc}/releases — create the release at the
//     given tag with description (= notes file body).
//  2. For each asset path: POST /assets/links — links each pre-uploaded
//     file. GitLab releases don't support direct file upload via the
//     release API; the calling job is expected to have published each
//     asset to the project's package registry first and pass URLs (not
//     filesystem paths). For now the adapter passes the asset basename
//     as both name and url placeholder; the calling workflow can
//     pre-stage uploads before invoking this.
//
// Mirrors the not-yet-implemented bash stub. GitLab parity for release
// creation is intentionally minimal — full asset-upload support is
// follow-up work for when the GitLab catalog wires real publishing.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return errors.New("CreateRelease: tag is empty")
	}
	if repo == "" {
		return errors.New("CreateRelease: repo is empty")
	}
	get := p.envFunc()
	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("CI_SERVER_URL")
	}
	if apiBase == "" {
		apiBase = "https://gitlab.com"
	}
	token := get("GITLAB_TOKEN")
	if token == "" {
		token = get("CI_JOB_TOKEN")
	}
	headers := map[string]string{
		"PRIVATE-TOKEN": token,
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
		"name":        firstNonEmpty(spec.Name, spec.Tag),
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
		linkPayload, _ := json.Marshal(map[string]any{
			"name": filepath.Base(asset),
			// `url` is required by GitLab's API; using a stub
			// based on the project URL when no real upload location
			// is available. Workflows that want real downloads upload
			// to the package registry first.
			"url": fmt.Sprintf("%s/%s/-/releases/%s/downloads/%s",
				strings.TrimRight(apiBase, "/"),
				repo, url.PathEscape(spec.Tag), url.PathEscape(filepath.Base(asset))),
		})
		linksEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded +
			"/releases/" + url.PathEscape(spec.Tag) + "/assets/links"
		_ = postJSON(ctx, p.HTTPClient, linksEndpoint, headers, linkPayload)
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	if client == nil {
		// Never use http.DefaultClient for outbound calls: zero-value
		// Timeout means hung connections block indefinitely.
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if cls := errs.FromHTTPStatus(resp.StatusCode); cls != nil {
			return fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, string(respBody), cls)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (p *Provider) envFunc() func(string) string {
	if p.Env != nil {
		return p.Env
	}
	return os.Getenv
}

func getJSON(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	if client == nil {
		// Never use http.DefaultClient for outbound calls: zero-value
		// Timeout means hung connections block indefinitely.
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if cls := errs.FromHTTPStatus(resp.StatusCode); cls != nil {
			return nil, fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, string(body), cls)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// UploadSARIF is unsupported on GitLab. GitLab CI consumes the
// JSON SAST report format (gl-sast-report.json) that trivy/opengrep
// emit directly via their --gitlab-sast-output flag — there is no
// REST endpoint that mirrors GitHub Code Scanning's SARIF surface.
func (p *Provider) UploadSARIF(_ context.Context, _ provider.SARIFUpload) error {
	return fmt.Errorf("SARIF upload is GitHub-only; use GitLab SAST report instead: %w", errs.ErrUnsupported)
}

// Compile-time conformance check.
var _ provider.Provider = (*Provider)(nil)
