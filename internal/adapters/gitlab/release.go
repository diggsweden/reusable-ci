// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// contentTypeJSON is the request Content-Type for GitLab's JSON endpoints.
const contentTypeJSON = "application/json"

// releasePayload builds the GitLab release create/update body shared by
// CreateRelease and PublishRelease so the two never drift. includeTag adds
// tag_name (create only; on update the tag is in the URL, not the body).
func releasePayload(spec provider.ReleaseSpec, desc string, includeTag bool) map[string]any {
	payload := map[string]any{
		"name":        cmp.Or(spec.Name, spec.Tag),
		"description": desc,
	}
	if includeTag {
		payload["tag_name"] = spec.Tag
	}

	return payload
}

// CreateRelease creates a GitLab release via REST API. Two-stage:
//
//  1. POST /api/v4/projects/{enc}/releases — create the release at the
//     given tag with description (= notes file body).
//  2. For each asset path: POST /projects/{enc}/uploads, then link the returned
//     project upload URL via POST /releases/{tag}/assets/links.
//
// An existing release for the tag is deleted first, so a re-run publishes
// cleanly (the tag itself is kept) — the same cleanup the github and forgejo
// adapters perform, and what `release publish --strategy recreate` promises:
// "recreate deletes and recreates it". Without it a second call to the same tag
// got HTTP 409 "Release already exists" here and nowhere else, which is a
// difference a caller is not supposed to be able to see. Found by the live
// conformance tier (PAR-REL-3); `--strategy reconcile` remains the in-place
// upsert for callers who want the release object preserved.
//
//nolint:cyclop // REST flow: delete-if-exists → create release → upload + link each asset.
func (p *Provider) CreateRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("CreateRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("CreateRelease: repo is empty: %w", errs.ErrUsage)
	}

	apiBase, headers := p.apiContext()
	headers["Content-Type"] = contentTypeJSON
	encoded := url.PathEscape(repo)
	endpoint := projectEndpoint(apiBase, repo) + "/releases"

	desc := spec.Name
	if spec.NotesFile != "" {
		body, err := os.ReadFile(spec.NotesFile)
		if err == nil {
			desc = string(body)
		}
	}

	payload, err := json.Marshal(releasePayload(spec, desc, true))
	if err != nil {
		return fmt.Errorf("marshal release payload: %w", err)
	}

	// Delete before create, not create-and-tolerate-409: GitLab's POST is not
	// an upsert, so a 409 would leave the previous release's notes and asset
	// links in place while reporting success.
	exists, err := p.releaseExists(ctx, endpoint, spec.Tag, headers)
	if err != nil {
		return err
	}

	if exists {
		if err := deleteJSON(ctx, p.HTTPClient, endpoint+"/"+url.PathEscape(spec.Tag), headers); err != nil {
			return fmt.Errorf("gitlab delete existing release %s: %w", spec.Tag, err)
		}
	}

	if err := postJSON(ctx, p.HTTPClient, endpoint, headers, payload); err != nil {
		return fmt.Errorf("gitlab create release: %w", err)
	}

	for _, asset := range spec.Assets {
		if err := p.uploadAndLinkReleaseAsset(ctx, apiBase, encoded, repo, spec.Tag, asset, headers); err != nil {
			return err
		}
	}

	return nil
}

// PublishRelease creates the release for spec.Tag if it is missing, or updates
// it and reconciles its asset links in place otherwise. Unlike CreateRelease it
// never deletes the release object: an existing release is updated via
// PUT /releases/:tag, colliding asset links are replaced by name, desired
// assets are uploaded and linked, and links no longer in spec.Assets are
// removed. It satisfies the provider.ReleasePublisher role.
func (p *Provider) PublishRelease(ctx context.Context, repo string, spec provider.ReleaseSpec) error {
	if spec.Tag == "" {
		return fmt.Errorf("PublishRelease: tag is empty: %w", errs.ErrUsage)
	}

	if repo == "" {
		return fmt.Errorf("PublishRelease: repo is empty: %w", errs.ErrUsage)
	}

	apiBase, headers := p.apiContext()
	encoded := url.PathEscape(repo)

	desc := spec.Name
	if spec.NotesFile != "" {
		if body, err := os.ReadFile(spec.NotesFile); err == nil {
			desc = string(body)
		}
	}

	if err := p.upsertRelease(ctx, apiBase, encoded, spec, desc, headers); err != nil {
		return err
	}

	for _, asset := range spec.Assets {
		if err := p.uploadAndLinkReleaseAsset(ctx, apiBase, encoded, repo, spec.Tag, asset, headers); err != nil {
			return err
		}
	}

	return p.deleteStaleReleaseLinks(ctx, apiBase, encoded, spec.Tag, spec.Assets, headers)
}

// upsertRelease creates the release when it does not yet exist, otherwise
// updates its name/description in place via PUT (never deleting it).
func (p *Provider) upsertRelease(ctx context.Context, apiBase, encoded string, spec provider.ReleaseSpec, desc string, headers map[string]string) error {
	releasesEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded + "/releases"

	jsonHeaders := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		jsonHeaders[key] = value
	}

	jsonHeaders["Content-Type"] = contentTypeJSON

	exists, err := p.releaseExists(ctx, releasesEndpoint, spec.Tag, headers)
	if err != nil {
		return err
	}

	if exists {
		payload, marshalErr := json.Marshal(releasePayload(spec, desc, false))
		if marshalErr != nil {
			return fmt.Errorf("marshal release update payload: %w", marshalErr)
		}

		endpoint := releasesEndpoint + "/" + url.PathEscape(spec.Tag)
		if err := putJSON(ctx, p.HTTPClient, endpoint, jsonHeaders, payload); err != nil {
			return fmt.Errorf("gitlab update release %s: %w", spec.Tag, err)
		}

		return nil
	}

	payload, marshalErr := json.Marshal(releasePayload(spec, desc, true))
	if marshalErr != nil {
		return fmt.Errorf("marshal release payload: %w", marshalErr)
	}

	if err := postJSON(ctx, p.HTTPClient, releasesEndpoint, jsonHeaders, payload); err != nil {
		return fmt.Errorf("gitlab create release: %w", err)
	}

	return nil
}

// releaseExists reports whether a release for tag already exists. A typed 404
// (errs.ErrMissingInput) means "no such release"; any other error propagates.
func (p *Provider) releaseExists(ctx context.Context, releasesEndpoint, tag string, headers map[string]string) (bool, error) {
	endpoint := releasesEndpoint + "/" + url.PathEscape(tag)

	if _, err := getJSON(ctx, p.HTTPClient, endpoint, headers); err != nil {
		if errors.Is(err, errs.ErrMissingInput) {
			return false, nil
		}

		return false, fmt.Errorf("gitlab check release %s: %w", tag, err)
	}

	return true, nil
}

// deleteStaleReleaseLinks removes every asset link whose name is not among the
// desired assets' basenames, so a reconcile publish converges the link set.
func (p *Provider) deleteStaleReleaseLinks(ctx context.Context, apiBase, encoded, tag string, desired []string, headers map[string]string) error {
	linksEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encoded +
		"/releases/" + url.PathEscape(tag) + "/assets/links"

	body, err := getJSON(ctx, p.HTTPClient, linksEndpoint, headers)
	if err != nil {
		return fmt.Errorf("gitlab list release asset links for %s: %w", tag, err)
	}

	var links []gitlabReleaseLink
	if err := json.Unmarshal(body, &links); err != nil {
		return fmt.Errorf("gitlab parse release asset links for %s: %w", tag, err)
	}

	desiredNames := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		desiredNames[filepath.Base(file)] = struct{}{}
	}

	for _, link := range links {
		if _, keep := desiredNames[link.Name]; keep {
			continue
		}

		deleteEndpoint := linksEndpoint + "/" + strconv.Itoa(link.ID)
		if err := deleteJSON(ctx, p.HTTPClient, deleteEndpoint, headers); err != nil {
			return fmt.Errorf("gitlab delete stale release asset link %q: %w", link.Name, err)
		}
	}

	return nil
}

// UploadReleaseAsset uploads a single file to GitLab project uploads, then links
// the returned URL to the release identified by tag. Existing release links with
// the same basename are deleted first to match GitHub's --clobber semantics.
func (p *Provider) UploadReleaseAsset(ctx context.Context, tag, file string) error {
	if tag == "" {
		return fmt.Errorf("UploadReleaseAsset: tag is empty: %w", errs.ErrUsage)
	}

	if file == "" {
		return fmt.Errorf("UploadReleaseAsset: file is empty: %w", errs.ErrUsage)
	}

	repo := p.envFunc()("CI_PROJECT_PATH")
	if repo == "" {
		return fmt.Errorf("UploadReleaseAsset: CI_PROJECT_PATH is required: %w", errs.ErrUsage)
	}

	apiBase, headers := p.apiContext()
	encoded := url.PathEscape(repo)

	return p.uploadAndLinkReleaseAsset(ctx, apiBase, encoded, repo, tag, file, headers)
}

func (p *Provider) uploadAndLinkReleaseAsset(ctx context.Context, apiBase, encodedProject, repo, tag, file string, headers map[string]string) error {
	name := filepath.Base(file)
	if err := p.deleteExistingReleaseLink(ctx, apiBase, encodedProject, tag, name, headers); err != nil {
		return err
	}

	assetURL, err := p.uploadProjectFile(ctx, apiBase, encodedProject, repo, file, headers)
	if err != nil {
		return err
	}

	return p.createReleaseLink(ctx, apiBase, encodedProject, tag, name, assetURL, headers)
}

type gitlabReleaseLink struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (p *Provider) deleteExistingReleaseLink(ctx context.Context, apiBase, encodedProject, tag, name string, headers map[string]string) error {
	linksEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encodedProject +
		"/releases/" + url.PathEscape(tag) + "/assets/links"

	body, err := getJSON(ctx, p.HTTPClient, linksEndpoint, headers)
	if err != nil {
		return fmt.Errorf("gitlab list release asset links for %s: %w", tag, err)
	}

	var links []gitlabReleaseLink
	if err := json.Unmarshal(body, &links); err != nil {
		return fmt.Errorf("gitlab parse release asset links for %s: %w", tag, err)
	}

	for _, link := range links {
		if link.Name != name {
			continue
		}

		deleteEndpoint := linksEndpoint + "/" + strconv.Itoa(link.ID)
		if err := deleteJSON(ctx, p.HTTPClient, deleteEndpoint, headers); err != nil {
			return fmt.Errorf("gitlab delete existing release asset link %q: %w", name, err)
		}
	}

	return nil
}

type gitlabUploadResponse struct {
	URL      string `json:"url"`
	FullPath string `json:"full_path"`
}

func (p *Provider) uploadProjectFile(ctx context.Context, apiBase, encodedProject, repo, file string, headers map[string]string) (string, error) {
	uploadEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encodedProject + "/uploads"

	body, err := postMultipartFile(ctx, p.HTTPClient, uploadEndpoint, headers, "file", file)
	if err != nil {
		return "", fmt.Errorf("gitlab upload release asset %q: %w", file, err)
	}

	var uploaded gitlabUploadResponse
	if err := json.Unmarshal(body, &uploaded); err != nil {
		return "", fmt.Errorf("gitlab parse upload response for %q: %w", file, err)
	}

	assetURL := gitlabUploadedAssetURL(apiBase, repo, uploaded)
	if assetURL == "" {
		return "", fmt.Errorf("gitlab upload response for %q did not include a usable URL: %w", file, errs.ErrMalformedInput)
	}

	return assetURL, nil
}

func gitlabUploadedAssetURL(apiBase, repo string, uploaded gitlabUploadResponse) string {
	for _, candidate := range []string{uploaded.FullPath, uploaded.URL} {
		if strings.HasPrefix(candidate, "https://") || strings.HasPrefix(candidate, "http://") {
			return candidate
		}
	}

	base := strings.TrimRight(apiBase, "/")
	if uploaded.FullPath != "" {
		return base + "/" + strings.TrimLeft(uploaded.FullPath, "/")
	}

	if uploaded.URL != "" {
		return base + "/" + strings.Trim(strings.Trim(repo, "/")+"/"+strings.TrimLeft(uploaded.URL, "/"), "/")
	}

	return ""
}

func (p *Provider) createReleaseLink(ctx context.Context, apiBase, encodedProject, tag, name, assetURL string, headers map[string]string) error {
	linkPayload, err := json.Marshal(map[string]any{"name": name, "url": assetURL})
	if err != nil {
		return fmt.Errorf("gitlab marshal release asset link %q: %w", name, err)
	}

	linkHeaders := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		linkHeaders[key] = value
	}

	linkHeaders["Content-Type"] = contentTypeJSON

	linksEndpoint := strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + encodedProject +
		"/releases/" + url.PathEscape(tag) + "/assets/links"
	if err := postJSON(ctx, p.HTTPClient, linksEndpoint, linkHeaders, linkPayload); err != nil {
		return fmt.Errorf("gitlab create release asset link %q: %w", name, err)
	}

	return nil
}

func postMultipartFile(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, field, file string) ([]byte, error) {
	if client == nil {
		client = defaultHTTPClient()
	}

	f, err := os.Open(file) //nolint:gosec,varnamelen // release asset path is an explicit operator-supplied file.
	if err != nil {
		return nil, fmt.Errorf("open release asset %q: %w", file, err)
	}

	defer func() { _ = f.Close() }()

	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()

		return nil, fmt.Errorf("build request: %w", err)
	}

	for k, v := range headers {
		if v == "" {
			continue
		}

		req.Header.Set(k, v)
	}

	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	go streamMultipartFile(writer, multipartWriter, field, file, f)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	return readMultipartResponse(resp)
}

// streamMultipartFile writes one form file into the multipart pipe, closing
// the pipe writer with the first error encountered.
func streamMultipartFile(writer *io.PipeWriter, multipartWriter *multipart.Writer, field, file string, contents io.Reader) {
	part, createErr := multipartWriter.CreateFormFile(field, filepath.Base(file))
	if createErr != nil {
		_ = writer.CloseWithError(createErr)

		return
	}

	if _, copyErr := io.Copy(part, contents); copyErr != nil {
		_ = writer.CloseWithError(copyErr)

		return
	}

	if closeErr := multipartWriter.Close(); closeErr != nil {
		_ = writer.CloseWithError(closeErr)

		return
	}

	_ = writer.Close()
}

// readMultipartResponse reads the response body, classifying non-2xx
// responses via errs.FromHTTPStatus when possible.
func readMultipartResponse(resp *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read body: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return nil, fmt.Errorf("HTTP %d: %s: %w", resp.StatusCode, string(body), cls)
	}

	return body, nil
}
