// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // protocol-mandated transport checksum (x-actions-results-md5), not security.
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// runArtifactAPIVersion is the Actions runtime artifact protocol version
// Forgejo implements (the v3 / Azure-pipelines container API). The shell
// download-consumer this replaces runs against this version on Codeberg.
const runArtifactAPIVersion = "6.0-preview"

// defaultForgejoRetentionDays is sent when the caller leaves retention
// unspecified. Forgejo's CreateArtifact rejects an absent RetentionDays with
// HTTP 500, so the upload always sends a positive value; "0 = forge default"
// therefore maps to this backend minimum. Callers wanting longer pass
// --retention-days.
const defaultForgejoRetentionDays = 1

// appendItemPath joins the itemPath query onto a container URL. Forgejo's
// CreateArtifact returns a fileContainerResourceUrl that already carries
// "?retentionDays=N", so itemPath must be appended with '&' when a query is
// already present — a naive second '?' leaves Forgejo unable to parse itemPath
// and it fails the upload as "Invalid artifact hash".
func appendItemPath(containerURL, itemPath string) string {
	sep := "?"
	if strings.Contains(containerURL, "?") {
		sep = "&"
	}

	return containerURL + sep + "itemPath=" + url.QueryEscape(itemPath)
}

// fileMD5Base64 returns base64(md5(content)) — the value Forgejo's upload
// handler matches against the chunk it receives via x-actions-results-md5
// ("md5 not match" otherwise). md5 here is the protocol's transport checksum,
// not a security primitive.
func fileMD5Base64(r io.Reader) (string, error) {
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
	hasher := md5.New() //nolint:gosec // protocol-mandated transport checksum, not security.
	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(hasher.Sum(nil)), nil
}

// DownloadRunArtifact fetches a current-run artifact by name via the
// Forgejo Actions runtime service: list artifacts → resolve the file
// container → download each entry. The runtime token is run-scoped, so a
// foreign RunID is reported as unsupported rather than silently served to the wrong run.
//
// Credentials live only in per-request Bearer headers (never argv, never
// disk); every entry path is gated by domain/artifact.SafeJoin and each
// file is size-capped.
func (p *Provider) DownloadRunArtifact(ctx context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	if in.Pattern == "" {
		if err := domainartifact.ValidateName(in.Name); err != nil {
			return provider.RunArtifactInfo{}, err
		}
	}

	if in.Dir == "" {
		return provider.RunArtifactInfo{}, fmt.Errorf("destination dir is required: %w", errs.ErrUsage)
	}

	// The run-id scoping check reads the run id directly rather than via
	// resolveRuntimeCreds, and must stay ahead of it: a caller naming a
	// different run gets "the token is scoped to this run" even when the run
	// id is absent entirely, which is the more useful of the two errors.
	runID := runcontext.RunID().Resolve(p.envFunc())
	if in.RunID != "" && in.RunID != runID {
		return provider.RunArtifactInfo{}, fmt.Errorf(
			"forgejo runtime token is scoped to the current run %q, cannot read run %q: %w",
			runID, in.RunID, errs.ErrUnsupported)
	}

	creds, err := p.resolveRuntimeCreds()
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	if in.Pattern != "" {
		return p.downloadMatchingContainers(ctx, creds, in)
	}

	containerURL, err := p.resolveContainerURL(ctx, creds, in.Name)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return p.downloadContainer(ctx, creds, containerURL, in.Name, in.Dir)
}

// downloadMatchingContainers implements pattern/merge-multiple for Forgejo:
// list the run's artifacts, keep those whose name matches the glob, and
// download each — flattened into Dir (MergeMultiple) or into Dir/<name>/.
func (p *Provider) downloadMatchingContainers(ctx context.Context, creds runtimeUploadCreds, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	matches, err := p.resolveMatchingContainers(ctx, creds, in.Pattern)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	var totalBytes int64

	var totalFiles int

	for _, match := range matches {
		// The artifact name comes from the forge; validate it (control chars /
		// invalid UTF-8) and resolve the per-artifact destination via SafeJoin
		// so a hostile name can't traverse out of Dir.
		if nameErr := domainartifact.ValidateName(match.name); nameErr != nil {
			return provider.RunArtifactInfo{}, fmt.Errorf("forge returned an unsafe artifact name: %w", nameErr)
		}

		dest := in.Dir

		if !in.MergeMultiple {
			safeDest, joinErr := domainartifact.SafeJoin(in.Dir, match.name)
			if joinErr != nil {
				return provider.RunArtifactInfo{}, joinErr
			}

			dest = safeDest
		}

		info, derr := p.downloadContainer(ctx, creds, match.url, match.name, dest)
		if derr != nil {
			return provider.RunArtifactInfo{}, derr
		}

		totalBytes += info.Bytes
		totalFiles += info.FileCount
	}

	return provider.RunArtifactInfo{Name: in.Pattern, Bytes: totalBytes, FileCount: totalFiles}, nil
}

func (c runtimeUploadCreds) validate() error {
	return provider.ValidateRunArtifactCreds(provider.RunArtifactCreds{
		Forge:    "forgejo",
		URLVar:   "ACTIONS_RUNTIME_URL",
		URLWhat:  "the runner's artifact service endpoint",
		TokenVar: "ACTIONS_RUNTIME_TOKEN",
		URL:      c.url,
		Token:    c.token,
	})
}

// artifactsURL is the run's artifact-list endpoint, which every call below
// derives identically from the endpoint and run id.
func (c runtimeUploadCreds) artifactsURL() string {
	return fmt.Sprintf("%s/_apis/pipelines/workflows/%s/artifacts?api-version=%s",
		c.url, c.runID, runArtifactAPIVersion)
}

// resolveContainerURL lists the run's artifacts and returns the file
// container URL for the exactly-one artifact matching name.
func (p *Provider) resolveContainerURL(ctx context.Context, creds runtimeUploadCreds, name string) (string, error) {
	listURL := creds.artifactsURL()

	var list struct {
		Value []struct {
			Name                     string `json:"name"`
			FileContainerResourceURL string `json:"fileContainerResourceUrl"`
		} `json:"value"`
	}

	if err := p.getRuntimeJSON(ctx, listURL, creds.token, &list); err != nil {
		return "", fmt.Errorf("list run %s artifacts: %w", creds.runID, err)
	}

	var matches []string

	for _, a := range list.Value {
		if a.Name == name {
			matches = append(matches, a.FileContainerResourceURL)
		}
	}

	switch len(matches) {
	case 1:
		if !strings.HasPrefix(matches[0], "http://") && !strings.HasPrefix(matches[0], "https://") {
			return "", fmt.Errorf("artifact %q has no usable file container URL: %w", name, errs.ErrValidation)
		}

		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no artifact named %q on run %s: %w", name, creds.runID, errs.ErrReleaseNotFound)
	default:
		return "", fmt.Errorf("%d artifacts named %q on run %s (expected exactly one): %w", len(matches), name, creds.runID, errs.ErrValidation)
	}
}

// containerMatch is one artifact's name and file-container URL.
type containerMatch struct {
	name string
	url  string
}

// resolveMatchingContainers lists the run's artifacts and returns every one
// whose name matches the glob pattern. Names carry no "/", so path.Match is
// the right matcher.
func (p *Provider) resolveMatchingContainers(ctx context.Context, creds runtimeUploadCreds, pattern string) ([]containerMatch, error) {
	listURL := creds.artifactsURL()

	var list struct {
		Value []struct {
			Name                     string `json:"name"`
			FileContainerResourceURL string `json:"fileContainerResourceUrl"`
		} `json:"value"`
	}

	if err := p.getRuntimeJSON(ctx, listURL, creds.token, &list); err != nil {
		return nil, fmt.Errorf("list run %s artifacts: %w", creds.runID, err)
	}

	var matches []containerMatch

	for _, art := range list.Value {
		ok, merr := path.Match(pattern, art.Name)
		if merr != nil {
			return nil, fmt.Errorf("invalid artifact pattern %q: %w", pattern, errs.ErrUsage)
		}

		if !ok {
			continue
		}

		if !strings.HasPrefix(art.FileContainerResourceURL, "http://") && !strings.HasPrefix(art.FileContainerResourceURL, "https://") {
			return nil, fmt.Errorf("artifact %q has no usable file container URL: %w", art.Name, errs.ErrValidation)
		}

		matches = append(matches, containerMatch{name: art.Name, url: art.FileContainerResourceURL})
	}

	return matches, nil
}

// downloadContainer lists the container's file entries and writes each one
// safely under dir, returning the byte/file totals.
func (p *Provider) downloadContainer(ctx context.Context, creds runtimeUploadCreds, containerURL, name, dir string) (provider.RunArtifactInfo, error) {
	itemURL := appendItemPath(containerURL, name)

	var container struct {
		Value []struct {
			Path            string `json:"path"`
			ItemType        string `json:"itemType"`
			ContentLocation string `json:"contentLocation"`
			FileLength      *int64 `json:"fileLength"`
		} `json:"value"`
	}

	if err := p.getRuntimeJSON(ctx, itemURL, creds.token, &container); err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("list artifact %q files: %w", name, err)
	}

	info := provider.RunArtifactInfo{Name: name}

	for _, entry := range container.Value {
		if entry.ItemType != "file" {
			continue
		}

		rel := strings.TrimPrefix(entry.Path, name+"/")

		dest, err := domainartifact.SafeJoin(dir, rel)
		if err != nil {
			return provider.RunArtifactInfo{}, err
		}

		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil { //nolint:gosec,mnd // artifact dirs read by downstream build steps.
			return provider.RunArtifactInfo{}, fmt.Errorf("mkdir %q: %w", filepath.Dir(dest), mkErr)
		}

		n, err := p.writeEntry(ctx, entry.ContentLocation, creds.token, dest, entry.FileLength)
		if err != nil {
			return provider.RunArtifactInfo{}, err
		}

		info.Bytes += n
		info.FileCount++
	}

	if info.FileCount == 0 {
		return provider.RunArtifactInfo{}, fmt.Errorf("artifact %q contains no files: %w", name, errs.ErrValidation)
	}

	return info, nil
}

// writeEntry materializes one container entry at dest, size-capped, and
// returns the bytes written. An empty contentLocation is only valid for a
// declared zero-length file; otherwise the entry is malformed.
func (p *Provider) writeEntry(ctx context.Context, contentLocation, token, dest string, fileLength *int64) (int64, error) {
	if contentLocation == "" {
		if fileLength != nil && *fileLength == 0 {
			if err := os.WriteFile(dest, nil, 0o600); err != nil {
				return 0, fmt.Errorf("write empty %q: %w", dest, err)
			}

			return 0, nil
		}

		return 0, fmt.Errorf("artifact entry %q has no content URL: %w", dest, errs.ErrValidation)
	}

	return p.downloadFile(ctx, contentLocation, token, dest)
}

// downloadFile GETs contentLocation with the runtime Bearer token and
// streams it to dest, bounded by MaxFileBytes.
func (p *Provider) downloadFile(ctx context.Context, contentLocation, token, dest string) (int64, error) {
	req, err := newRuntimeRequest(ctx, contentLocation, token, "application/octet-stream;api-version="+runArtifactAPIVersion)
	if err != nil {
		return 0, err
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("download artifact entry: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download artifact entry: HTTP %d: %w", resp.StatusCode, classifyStatus(resp.StatusCode))
	}

	// dest is validated by domain/artifact.SafeJoin in the caller.
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // dest is SafeJoin-checked.
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", dest, err)
	}

	written, copyErr := io.CopyN(out, resp.Body, domainartifact.MaxFileBytes)
	closeErr := out.Close()

	switch {
	case copyErr != nil && !errors.Is(copyErr, io.EOF):
		return 0, fmt.Errorf("write %q: %w", dest, copyErr)
	case closeErr != nil:
		return 0, fmt.Errorf("close %q: %w", dest, closeErr)
	}

	return written, nil
}

// getRuntimeJSON GETs url with the runtime Bearer token and decodes JSON.
func (p *Provider) getRuntimeJSON(ctx context.Context, url, token string, out any) error {
	req, err := newRuntimeRequest(ctx, url, token, "application/json;api-version="+runArtifactAPIVersion)
	if err != nil {
		return err
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("runtime request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runtime request: HTTP %d: %w", resp.StatusCode, classifyStatus(resp.StatusCode))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode runtime response: %w", err)
	}

	return nil
}

// newRuntimeRequest builds a GET carrying the runtime Bearer token in a
// per-request header — the token never reaches argv or disk.
func newRuntimeRequest(ctx context.Context, rawURL, token, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)

	return req, nil
}

func classifyStatus(code int) error {
	if cls := errs.FromHTTPStatus(code); cls != nil {
		return cls
	}

	return errs.ErrDependencyUnavailable
}

func is2xx(code int) bool { return code >= http.StatusOK && code < http.StatusMultipleChoices }

// uploadFile is one resolved upload entry: where it is on disk, the
// artifact-relative item path the runtime stores it under, and its size.
type uploadFile struct {
	abs      string
	itemPath string
	size     int64
}

// UploadRunArtifact uploads files as a named artifact into the current run
// via the Forgejo Actions runtime service (v3 container protocol): create
// the file container, PUT each file, then finalize the total size. Same
// credential discipline as download — the runtime Bearer token lives only
// in per-request headers.
func (p *Provider) UploadRunArtifact(ctx context.Context, in provider.RunArtifactUpload) (provider.RunArtifactInfo, error) {
	if err := domainartifact.ValidateName(in.Name); err != nil {
		return provider.RunArtifactInfo{}, err
	}

	files, err := collectUploadFiles(in)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	if len(files) == 0 {
		return p.handleNoFiles(in)
	}

	creds, err := p.resolveRuntimeCreds()
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = defaultForgejoRetentionDays
	}

	containerURL, err := p.createContainer(ctx, creds, in.Name, retentionDays)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	var total int64

	for _, item := range files {
		if err := p.putFile(ctx, creds, containerURL, item); err != nil {
			return provider.RunArtifactInfo{}, err
		}

		total += item.size
	}

	if err := p.finalizeArtifact(ctx, creds, in.Name, total); err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: in.Name, Bytes: total, FileCount: len(files)}, nil
}

// runtimeUploadCreds are the Actions runtime endpoint, token, and run id the
// artifact upload needs, resolved from the runner environment.
type runtimeUploadCreds struct {
	url   string
	token string
	runID string
}

// resolveRuntimeCreds reads and validates the Actions runtime credentials from
// the runner environment.
func (p *Provider) resolveRuntimeCreds() (runtimeUploadCreds, error) {
	env := p.envFunc()

	runID := runcontext.RunID().Resolve(env)
	if runID == "" {
		return runtimeUploadCreds{}, fmt.Errorf("FORGEJO_RUN_ID/GITHUB_RUN_ID is required: %w", errs.ErrUsage)
	}

	creds := runtimeUploadCreds{
		url:   strings.TrimRight(env("ACTIONS_RUNTIME_URL"), "/"),
		token: env("ACTIONS_RUNTIME_TOKEN"),
		runID: runID,
	}

	if err := creds.validate(); err != nil {
		return runtimeUploadCreds{}, err
	}

	return creds, nil
}

// handleNoFiles applies the IfNoFiles policy when nothing matched.
func (p *Provider) handleNoFiles(in provider.RunArtifactUpload) (provider.RunArtifactInfo, error) {
	switch in.IfNoFiles {
	case provider.IfNoFilesWarn, provider.IfNoFilesIgnore:
		return provider.RunArtifactInfo{Name: in.Name}, nil
	default:
		return provider.RunArtifactInfo{}, fmt.Errorf("no files matched for artifact %q: %w", in.Name, errs.ErrValidation)
	}
}

// collectUploadFiles resolves the upload set via the shared collector and
// maps each entry to the Forgejo item path (artifact-name + relative
// path). The collection (walk/flatten, symlink-skip) is forge-neutral and
// lives in domain/artifact; only the item-path shaping is Forgejo's.
func collectUploadFiles(in provider.RunArtifactUpload) ([]uploadFile, error) {
	entries, err := domainartifact.CollectUpload(in.Dir, in.Files, in.Paths, in.IncludeHidden)
	if err != nil {
		return nil, err
	}

	out := make([]uploadFile, 0, len(entries))
	for _, entry := range entries {
		out = append(out, uploadFile{
			abs:      entry.Abs,
			itemPath: in.Name + "/" + entry.RelPath,
			size:     entry.Size,
		})
	}

	return out, nil
}

// createContainer POSTs the artifact creation request and returns the file
// container URL the per-file PUTs target.
func (p *Provider) createContainer(ctx context.Context, creds runtimeUploadCreds, name string, retentionDays int) (string, error) {
	body, _ := json.Marshal(struct { //nolint:errchkjson // fixed-shape struct is always encodable.
		Type          string `json:"Type"`
		Name          string `json:"Name"`
		RetentionDays int    `json:"RetentionDays"`
	}{Type: "actions_storage", Name: name, RetentionDays: retentionDays})
	createURL := creds.artifactsURL()

	resp, err := p.runtimeSend(ctx, http.MethodPost, createURL, creds.token, "application/json", bytes.NewReader(body), int64(len(body)), nil)
	if err != nil {
		return "", fmt.Errorf("create artifact container: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if !is2xx(resp.StatusCode) {
		return "", fmt.Errorf("create artifact container: HTTP %d: %w", resp.StatusCode, classifyStatus(resp.StatusCode))
	}

	var created struct {
		FileContainerResourceURL string `json:"fileContainerResourceUrl"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode create response: %w", err)
	}

	if !strings.HasPrefix(created.FileContainerResourceURL, "http://") && !strings.HasPrefix(created.FileContainerResourceURL, "https://") {
		return "", fmt.Errorf("artifact container has no usable URL: %w", errs.ErrValidation)
	}

	return created.FileContainerResourceURL, nil
}

// putFile streams one file into the container at its item path. It sends the
// whole-file Content-Range (the empty-file form "bytes 0--1/0" for zero-byte
// files, which Forgejo's range parser requires) plus the x-actions-results-md5
// digest of the body, which Forgejo verifies against the bytes it receives.
func (p *Provider) putFile(ctx context.Context, creds runtimeUploadCreds, containerURL string, item uploadFile) error {
	file, err := os.Open(item.abs) //nolint:gosec // item.abs comes from a caller-provided dir/file list.
	if err != nil {
		return fmt.Errorf("open %q: %w", item.abs, err)
	}

	defer func() { _ = file.Close() }()

	md5b64, err := fileMD5Base64(file)
	if err != nil {
		return fmt.Errorf("md5 %q: %w", item.abs, err)
	}

	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("rewind %q: %w", item.abs, seekErr)
	}

	headers := map[string]string{"x-actions-results-md5": md5b64}
	if item.size > 0 {
		headers["Content-Range"] = fmt.Sprintf("bytes 0-%d/%d", item.size-1, item.size)
	} else {
		// Forgejo's saveUploadChunk Sscanf's Content-Range and 500s ("Error save
		// upload chunk") on a missing one; the canonical client sends start 0,
		// end uploadFileSize-1 = -1, total 0 for a zero-byte file.
		headers["Content-Range"] = "bytes 0--1/0"
	}

	putURL := appendItemPath(containerURL, item.itemPath)

	resp, err := p.runtimeSend(ctx, http.MethodPut, putURL, creds.token, "application/octet-stream", file, item.size, headers)
	if err != nil {
		return fmt.Errorf("upload %q: %w", item.itemPath, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if !is2xx(resp.StatusCode) {
		return fmt.Errorf("upload %q: HTTP %d: %w", item.itemPath, resp.StatusCode, classifyStatus(resp.StatusCode))
	}

	return nil
}

// finalizeArtifact PATCHes the total size, sealing the artifact.
func (p *Provider) finalizeArtifact(ctx context.Context, creds runtimeUploadCreds, name string, total int64) error {
	body, _ := json.Marshal(map[string]int64{"Size": total}) //nolint:errchkjson // single int field is always encodable.
	patchURL := fmt.Sprintf("%s/_apis/pipelines/workflows/%s/artifacts?artifactName=%s&api-version=%s",
		creds.url, creds.runID, url.QueryEscape(name), runArtifactAPIVersion)

	resp, err := p.runtimeSend(ctx, http.MethodPatch, patchURL, creds.token, "application/json", bytes.NewReader(body), int64(len(body)), nil)
	if err != nil {
		return fmt.Errorf("finalize artifact %q: %w", name, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if !is2xx(resp.StatusCode) {
		return fmt.Errorf("finalize artifact %q: HTTP %d: %w", name, resp.StatusCode, classifyStatus(resp.StatusCode))
	}

	return nil
}

// runtimeSend issues a request to the runtime service carrying the Bearer
// token in a per-request header — never argv, never disk.
func (p *Provider) runtimeSend(ctx context.Context, method, rawURL, token, contentType string, body io.Reader, contentLength int64, headers map[string]string) (*http.Response, error) {
	// ContentLength 0 with a non-nil body means "unknown" to net/http,
	// which then sends Transfer-Encoding: chunked — Forgejo's upload-chunk
	// handler 500s on that. NoBody makes the zero-length case an explicit
	// Content-Length: 0, the shape the canonical clients send. Hit in the
	// wild by the first live Codeberg round-trip: the zero-byte fixture
	// file failed while every sized file was fine.
	if contentLength == 0 {
		body = http.NoBody
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.ContentLength = contentLength
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json;api-version="+runArtifactAPIVersion)

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return p.httpClient().Do(req) //nolint:wrapcheck // callers wrap with operation context.
}
