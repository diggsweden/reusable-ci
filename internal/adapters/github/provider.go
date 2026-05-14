// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package github implements provider.Provider for GitHub Actions.
package github

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainlog "github.com/diggsweden/reusable-ci/internal/domain/log"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Well-known GitHub REST API constants. Internal to this adapter —
// other packages talk to GitHub through the Provider interface, not
// by composing URLs from these constants.
const (
	// defaultAPIBase is the GitHub REST API root used as a fallback
	// when no explicit override is supplied. GitHub Enterprise
	// deployments override this to their on-prem endpoint.
	defaultAPIBase = "https://api.github.com"

	// acceptJSONHeader is the canonical Accept header for the GitHub
	// REST API. Required on every request to get the JSON-shape
	// responses we parse.
	acceptJSONHeader = "application/vnd.github+json"

	// apiVersionHeader pins X-GitHub-Api-Version. Bump after
	// validating the new contract against this adapter's expected
	// response shapes.
	apiVersionHeader = "2022-11-28"
)

// Provider satisfies provider.Provider for the GitHub Actions runtime.
type Provider struct {
	// Env holds the env-var snapshot used for resolution. Empty values
	// fall back to os.Getenv at the time ResolveContext is called.
	// Tests substitute a fixed map.
	Env func(string) string

	// HTTPClient overrides http.DefaultClient. Tests inject an
	// httptest-backed client; production leaves it nil.
	HTTPClient *http.Client

	// APIBaseOverride overrides the default https://api.github.com.
	// Production normally reads $GITHUB_API_URL via Env. Tests set this
	// directly.
	APIBaseOverride string

	// GHBin overrides the `gh` CLI path. Empty → exec.LookPath("gh").
	// Used by CreateRelease; tests set it to a mockbinary stub.
	GHBin string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider {
	return &Provider{Env: os.Getenv}
}

// Name reports the platform identifier.
func (p *Provider) Name() provider.Platform { return provider.PlatformGitHub }

// pullRefPattern extracts the PR number from a refs/pull/<n>/{head,merge} ref.
var pullRefPattern = regexp.MustCompile(`^refs/pull/(\d+)/`)

// ResolveContext reads the canonical GitHub Actions env vars and returns
// a typed EventContext. No network, no I/O.
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.envFunc()

	sha := get("GITHUB_SHA")
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	refName := get("GITHUB_REF_NAME")
	refType := classifyRefType(get)

	branch := get("GITHUB_HEAD_REF") // populated on pull_request events
	if branch == "" && refType == provider.RefTypeBranch {
		branch = refName
	}

	prNumber := ""
	if refType == provider.RefTypePR {
		if m := pullRefPattern.FindStringSubmatch(get("GITHUB_REF")); len(m) == 2 {
			prNumber = m[1]
		}
	}

	repo := get("GITHUB_REPOSITORY")
	server := get("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	repoURL := ""
	if repo != "" {
		repoURL = strings.TrimRight(server, "/") + "/" + repo
	}

	return &provider.EventContext{
		Platform:  provider.PlatformGitHub,
		RefName:   refName,
		RefType:   refType,
		SHA:       sha,
		ShortSHA:  short,
		Branch:    branch,
		PRNumber:  prNumber,
		EventName: get("GITHUB_EVENT_NAME"),
		Repo:      repo,
		RepoURL:   repoURL,
	}, nil
}

// FetchRepoMetadata calls `GET /repos/{repo}` on the GitHub REST API and
// returns the description / html_url / SPDX license id. The token is
// optional — public repos work unauthenticated (with reduced rate limits).
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
		"Accept":               acceptJSONHeader,
		"X-GitHub-Api-Version": apiVersionHeader,
		"Authorization":        bearerHeader(get("GITHUB_TOKEN")),
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
	var r ghRepo
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("github decode repo response: %w", err)
	}
	return &provider.RepoMetadata{
		Description: r.Description,
		HTMLURL:     r.HTMLURL,
		LicenseSPDX: r.License.SPDXID,
	}, nil
}

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
		"Accept":               acceptJSONHeader,
		"X-GitHub-Api-Version": apiVersionHeader,
		"Authorization":        bearerHeader(token),
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
	var bp provider.BotPermissions
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); bp.UserAccessible = probe("/user") }()
	go func() { defer wg.Done(); bp.RepoAccessible = probe("/repos/" + repo) }()
	go func() { defer wg.Done(); bp.BranchesAccessible = probe("/repos/" + repo + "/branches") }()
	wg.Wait()
	return &bp, nil
}

// CreateRelease shells out to `gh release create` matching the bash's
// semantics: existing draft/prerelease tag → delete-and-recreate;
// existing stable tag → error (the bash exits 1 with "Cannot
// overwrite", same here). Asset list, notes file, --draft, --prerelease,
// --latest=false flags map directly from ReleaseSpec.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return errors.New("CreateRelease: tag is empty")
	}
	if repo == "" {
		return errors.New("CreateRelease: repo is empty")
	}

	if err := p.cleanupExistingRelease(ctx, spec.Tag); err != nil {
		return err
	}

	args := []string{"release", "create", spec.Tag}
	name := spec.Name
	if name == "" {
		name = spec.Tag
	}
	args = append(args, "--title", name)
	if spec.Draft {
		args = append(args, "--draft")
	}
	if spec.Prerelease {
		args = append(args, "--prerelease")
	}
	if !spec.MakeLatest {
		args = append(args, "--latest=false")
	}
	if spec.NotesFile != "" {
		args = append(args, "--notes-file", spec.NotesFile)
	}
	args = append(args, spec.Assets...)
	if _, err := p.runGH(ctx, args...); err != nil {
		return fmt.Errorf("gh release create: %w", err)
	}
	return nil
}

// cleanupExistingRelease implements the bash's `cleanup_existing_release`:
// if a release for `tag` already exists and is draft or prerelease, delete
// it; if it's stable, error out. Missing release → no-op.
func (p *Provider) cleanupExistingRelease(ctx context.Context, tag string) error {
	out, err := p.runGH(ctx, "release", "view", tag, "--json", "isDraft,isPrerelease")
	if err != nil {
		// `gh release view` exits non-zero both for "release doesn't
		// exist" (the expected path here) and for auth/network failures.
		// Map the first via classifyGHError → errs.ErrReleaseNotFound so
		// we can distinguish; propagate everything else.
		if errors.Is(classifyGHError(err), errs.ErrReleaseNotFound) {
			return nil
		}
		return fmt.Errorf("check existing release %q: %w", tag, err)
	}
	var info struct {
		IsDraft      bool `json:"isDraft"`
		IsPrerelease bool `json:"isPrerelease"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return fmt.Errorf("decode `gh release view` output: %w", err)
	}
	if info.IsDraft || info.IsPrerelease {
		if _, err := p.runGH(ctx, "release", "delete", tag, "--yes"); err != nil {
			return fmt.Errorf("delete existing draft/prerelease tag %q: %w", tag, err)
		}
		return nil
	}
	return fmt.Errorf("Release %s already exists and is not a draft/prerelease. Cannot overwrite.", tag)
}

// classifyGHError inspects a runGH failure and returns a sentinel from
// internal/domain/errs when the failure shape is recognised. Today this
// only covers "release not found" (HTTP 404 from `gh release view`).
// Callers that care use errors.Is to branch; callers that don't keep
// propagating the original wrapped error.
func classifyGHError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// gh prints "release not found" on the 404 path; older versions emit
	// "Not Found (HTTP 404)" via the API surface — match both.
	if strings.Contains(msg, "release not found") || strings.Contains(msg, "HTTP 404") {
		return errs.ErrReleaseNotFound
	}
	return err
}

// runGH executes `gh <args...>` and returns trimmed stdout. Combined
// stderr is included in the error on non-zero exit.
func (p *Provider) runGH(ctx context.Context, args ...string) (string, error) {
	bin := p.GHBin
	if bin == "" {
		bin = "gh"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	domainlog.TraceContext(ctx, "runGH: invoking gh", "bin", bin, "args", args)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// envFunc returns the env-getter, defaulting to os.Getenv.
func (p *Provider) envFunc() func(string) string {
	if p.Env != nil {
		return p.Env
	}
	return os.Getenv
}

// classifyRefType maps GITHUB_REF_TYPE / GITHUB_REF / GITHUB_EVENT_NAME
// to one of provider.RefType{Branch,Tag,PR,Other}.
//
// GitHub's GITHUB_REF_TYPE only ever returns "branch" or "tag"; it
// reports "branch" on pull_request events too. We override that to
// PR when the event is a pull_request{,_target,_review,…} so the
// EventContext distinguishes the three cases the rest of the code needs.
func classifyRefType(get func(string) string) provider.RefType {
	if strings.HasPrefix(get("GITHUB_EVENT_NAME"), "pull_request") {
		return provider.RefTypePR
	}
	switch get("GITHUB_REF_TYPE") {
	case "tag":
		return provider.RefTypeTag
	case "branch":
		return provider.RefTypeBranch
	}
	return provider.RefTypeOther
}

// bearerHeader returns "Bearer <token>" or "" when token is empty.
func bearerHeader(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

// getJSON does a GET with the provided headers, returns the body bytes.
// Empty Authorization is dropped. Caller is responsible for unmarshalling.
func getJSON(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	if client == nil {
		// Never use http.DefaultClient for outbound calls: zero-value
		// Timeout means hung connections block indefinitely. Match the
		// 30s default postJSON already uses below.
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

// UploadSARIF posts a SARIF report to the GitHub Code Scanning API.
// The SARIF body is gzip-then-base64 encoded as the API requires, then
// wrapped in the JSON payload shape {commit_sha, ref, sarif, tool_name?}.
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
	if up.Category != "" {
		payload["tool_name"] = up.Category
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
		"Authorization": "token " + up.Token,
		"Accept":        acceptJSONHeader,
		"Content-Type":  "application/json",
	}, body)
}

// gzipBase64 returns base64(gzip(in)) — the encoding the GitHub
// Code Scanning API requires for SARIF uploads.
func gzipBase64(in []byte) (string, error) {
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write(in); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gz.Bytes()), nil
}

// postJSON sends a POST request with the given body and headers,
// classifying non-2xx responses via errs.FromHTTPStatus when possible.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	if client == nil {
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
		return fmt.Errorf("post: %w", err)
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

// Compile-time conformance check.
var _ provider.Provider = (*Provider)(nil)
