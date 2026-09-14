// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// GitHub Actions artifact v4 upload. Unlike download (the plain REST
// artifacts API), upload is the "results" service: two Twirp JSON RPCs
// around an Azure block-blob PUT of the zipped artifact.
//
//	CreateArtifact  → a signed Azure SAS upload URL
//	PUT <sas>       → the zip (block blob)
//	FinalizeArtifact(size, sha256) → the artifact id
//
// The two backend ids come from the ACTIONS_RUNTIME_TOKEN JWT's `scp`
// claim (Actions.Results:<run>:<job>). Field names below are the
// protobuf-JSON (lowerCamelCase) names of github.actions.results.api.v1;
// keep them verbatim — they are the wire contract.
//
// Implemented from the documented protocol (no Go client library exists
// for v4 upload, and pulling protobuf/twirp codegen for two frozen calls
// is not worth it); the wire shape is unit-tested against mocks here, and
// confirmed end-to-end only on a real GitHub runner.
const (
	twirpCreateArtifact   = "twirp/github.actions.results.api.v1.ArtifactService/CreateArtifact"
	twirpFinalizeArtifact = "twirp/github.actions.results.api.v1.ArtifactService/FinalizeArtifact"

	// azureBlobAPIVersion is the x-ms-version the SAS block-blob PUT
	// advertises; any recent stable version is accepted by the backend.
	azureBlobAPIVersion = "2024-05-04"

	// maxUploadZipBytes bounds the single-PUT block blob. Above this the
	// protocol needs staged blocks (comp=block/blocklist); we fail clearly
	// rather than silently truncate.
	maxUploadZipBytes int64 = 256 << 20
)

// zipArchive carries the on-disk zip plus the totals the protocol and the
// caller need.
type zipArchive struct {
	path         string
	zipSize      int64
	sha256Hex    string
	uncompressed int64
	fileCount    int
}

// UploadRunArtifact implements provider.RunArtifactUploader via the v4
// results service. Credentials (the runtime JWT) live only in per-request
// Bearer headers; files are zipped through the shared collector so the
// same symlink/size guarantees as elsewhere apply.
func (p *Provider) UploadRunArtifact(ctx context.Context, in provider.RunArtifactUpload) (provider.RunArtifactInfo, error) {
	if err := domainartifact.ValidateName(in.Name); err != nil {
		return provider.RunArtifactInfo{}, err
	}

	entries, err := domainartifact.CollectUpload(in.Dir, in.Files, in.Paths, in.IncludeHidden)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	if len(entries) == 0 {
		if in.IfNoFiles == provider.IfNoFilesWarn || in.IfNoFiles == provider.IfNoFilesIgnore {
			return provider.RunArtifactInfo{Name: in.Name}, nil
		}

		return provider.RunArtifactInfo{}, domainartifact.NoFilesError(in.Name)
	}

	rt, err := p.resolveUploadContext()
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	archive, err := zipEntries(entries)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	defer func() { _ = os.Remove(archive.path) }()

	if archive.zipSize > maxUploadZipBytes {
		return provider.RunArtifactInfo{}, fmt.Errorf("artifact zip is %d bytes, over the %d single-PUT limit: %w", archive.zipSize, maxUploadZipBytes, errs.ErrUnsupported)
	}

	artifactID, err := p.runV4Upload(ctx, rt, in.Name, in.RetentionDays, archive)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: in.Name, ID: artifactID, Bytes: archive.uncompressed, FileCount: archive.fileCount}, nil
}

// uploadContext is the resolved v4 results endpoint + auth for one upload.
type uploadContext struct {
	resultsURL string
	token      string
	runID      string
	jobID      string
}

// resolveUploadContext reads and validates the results-service env and
// extracts the backend ids from the runtime token.
func (p *Provider) resolveUploadContext() (uploadContext, error) {
	env := p.envFunc()
	rt := uploadContext{
		resultsURL: strings.TrimRight(env("ACTIONS_RESULTS_URL"), "/"),
		token:      env("ACTIONS_RUNTIME_TOKEN"),
	}

	if err := validateResultsCreds(rt.resultsURL, rt.token); err != nil {
		return uploadContext{}, err
	}

	runID, jobID, err := backendIDsFromToken(rt.token)
	if err != nil {
		return uploadContext{}, err
	}

	rt.runID, rt.jobID = runID, jobID

	return rt, nil
}

// runV4Upload performs the three protocol stages: create → blob PUT →
// finalize, returning the artifact id.
func (p *Provider) runV4Upload(ctx context.Context, rt uploadContext, name string, retentionDays int, archive zipArchive) (string, error) {
	signedURL, err := p.createArtifact(ctx, rt, name, p.artifactExpiry(retentionDays, time.Now()))
	if err != nil {
		return "", err
	}

	if err := p.uploadZipBlob(ctx, signedURL, archive); err != nil {
		return "", err
	}

	return p.finalizeArtifact(ctx, rt, name, archive)
}

func validateResultsCreds(resultsURL, token string) error {
	return provider.ValidateRunArtifactCreds(provider.RunArtifactCreds{
		Forge:    "github",
		URLVar:   "ACTIONS_RESULTS_URL",
		URLWhat:  "the runner's artifact results endpoint",
		TokenVar: "ACTIONS_RUNTIME_TOKEN",
		URL:      resultsURL,
		Token:    token,
	})
}

// backendIDsFromToken extracts the workflow run/job backend ids from the
// runtime JWT's `scp` claim (a space-separated scope list containing
// "Actions.Results:<run>:<job>"). Only the payload is decoded — the token
// is our own runner credential, so no signature verification is needed.
func backendIDsFromToken(token string) (string, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("ACTIONS_RUNTIME_TOKEN is not a JWT: %w", errs.ErrValidation)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("decode runtime token payload: %w", errs.ErrValidation)
	}

	var claims struct {
		Scp string `json:"scp"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", fmt.Errorf("parse runtime token claims: %w", errs.ErrValidation)
	}

	for _, scope := range strings.Fields(claims.Scp) {
		rest, ok := strings.CutPrefix(scope, "Actions.Results:")
		if !ok {
			continue
		}

		run, job, ok := strings.Cut(rest, ":")
		if ok && run != "" && job != "" {
			return run, job, nil
		}
	}

	return "", "", fmt.Errorf("runtime token has no Actions.Results scope: %w", errs.ErrValidation)
}

// zipEntries writes the entries into a temp zip and reports its size,
// sha256, and the uncompressed/file totals. Each file is size-capped.
func zipEntries(entries []domainartifact.UploadEntry) (zipArchive, error) {
	tmp, err := os.CreateTemp("", "reusable-ci-upload-*.zip")
	if err != nil {
		return zipArchive{}, fmt.Errorf("create temp zip: %w", err)
	}

	out := zipArchive{path: tmp.Name(), fileCount: len(entries)}

	complete := false
	defer func() {
		if !complete {
			_ = tmp.Close()
			_ = os.Remove(out.path)
		}
	}()

	zw := zip.NewWriter(tmp)

	for _, entry := range entries {
		written, werr := writeZipEntry(zw, entry)
		if werr != nil {
			_ = zw.Close()
			_ = tmp.Close()

			return zipArchive{}, werr
		}

		out.uncompressed += written
	}

	if cerr := zw.Close(); cerr != nil {
		_ = tmp.Close()

		return zipArchive{}, fmt.Errorf("finalize zip: %w", cerr)
	}

	if cerr := tmp.Close(); cerr != nil {
		return zipArchive{}, fmt.Errorf("close zip: %w", cerr)
	}

	if err := hashAndSize(&out); err != nil {
		return zipArchive{}, err
	}

	complete = true

	return out, nil
}

func writeZipEntry(zw *zip.Writer, entry domainartifact.UploadEntry) (int64, error) {
	src, err := domainartifact.OpenUploadEntry(entry)
	if err != nil {
		return 0, err
	}

	defer func() { _ = src.Close() }()

	dst, err := zw.Create(entry.RelPath)
	if err != nil {
		return 0, fmt.Errorf("zip entry %q: %w", entry.RelPath, err)
	}

	written, copyErr := domainartifact.CopyAtMost(dst, src, domainartifact.MaxFileBytes)
	if copyErr != nil {
		return 0, fmt.Errorf("zip %q: %w", entry.RelPath, copyErr)
	}

	return written, nil
}

// hashAndSize fills the archive's zipSize and sha256Hex by reading the
// written file once.
func hashAndSize(out *zipArchive) error {
	file, err := os.Open(out.path) //nolint:gosec // out.path is our own CreateTemp file.
	if err != nil {
		return fmt.Errorf("reopen zip: %w", err)
	}

	defer func() { _ = file.Close() }()

	hasher := sha256.New()

	size, err := io.Copy(hasher, file)
	if err != nil {
		return fmt.Errorf("hash zip: %w", err)
	}

	out.zipSize = size
	out.sha256Hex = hex.EncodeToString(hasher.Sum(nil))

	return nil
}

type backendIDs struct {
	WorkflowRunBackendID    string `json:"workflowRunBackendId"`
	WorkflowJobRunBackendID string `json:"workflowJobRunBackendId"`
}

// artifactExpiry turns a requested retention into the CreateArtifact
// expiresAt timestamp, as actions/upload-artifact does: zero leaves the
// repository default, and a request beyond the repository's
// $GITHUB_RETENTION_DAYS is capped to it rather than refused.
func (p *Provider) artifactExpiry(retentionDays int, now time.Time) string {
	if retentionDays <= 0 {
		return ""
	}

	if maxDays, err := strconv.Atoi(p.envFunc()("GITHUB_RETENTION_DAYS")); err == nil && maxDays > 0 && retentionDays > maxDays {
		retentionDays = maxDays
	}

	return now.UTC().AddDate(0, 0, retentionDays).Format(time.RFC3339)
}

func (p *Provider) createArtifact(ctx context.Context, rt uploadContext, name, expiresAt string) (string, error) {
	req := struct {
		backendIDs
		Name      string `json:"name"`
		Version   int    `json:"version"`
		ExpiresAt string `json:"expiresAt,omitempty"`
	}{backendIDs{rt.runID, rt.jobID}, name, 4, expiresAt}

	var resp struct {
		OK              bool   `json:"ok"`
		SignedUploadURL string `json:"signedUploadUrl"`
	}

	if err := p.twirpPOST(ctx, rt.resultsURL, twirpCreateArtifact, rt.token, req, &resp); err != nil {
		return "", err
	}

	if !usableSignedUploadURL(resp.SignedUploadURL, rt.resultsURL) {
		return "", fmt.Errorf("CreateArtifact returned no usable upload URL: %w", errs.ErrValidation)
	}

	return resp.SignedUploadURL, nil
}

// usableSignedUploadURL accepts the blob URL CreateArtifact hands back only if
// it is an absolute URL with a host and no embedded credentials, and does not
// downgrade the transport: a results service reached over HTTPS may not send
// the artifact over plain HTTP. A results URL that is itself HTTP (a
// self-hosted runner on a private network) allows an HTTP blob URL. The blob
// PUT never carries the runtime token; the signature is in the URL.
func usableSignedUploadURL(signedURL, resultsURL string) bool {
	parsed, err := url.Parse(signedURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}

	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return strings.HasPrefix(resultsURL, "http://")
	default:
		return false
	}
}

func (p *Provider) finalizeArtifact(ctx context.Context, rt uploadContext, name string, archive zipArchive) (string, error) {
	req := struct {
		backendIDs
		Name string `json:"name"`
		Size int64  `json:"size"`
		Hash string `json:"hash"`
	}{backendIDs{rt.runID, rt.jobID}, name, archive.zipSize, "sha256:" + archive.sha256Hex}

	var resp struct {
		OK         bool   `json:"ok"`
		ArtifactID string `json:"artifactId"`
	}

	if err := p.twirpPOST(ctx, rt.resultsURL, twirpFinalizeArtifact, rt.token, req, &resp); err != nil {
		return "", err
	}

	if !resp.OK || strings.TrimSpace(resp.ArtifactID) == "" {
		return "", fmt.Errorf("FinalizeArtifact did not confirm success with an artifact id: %w", errs.ErrMalformedInput)
	}

	return resp.ArtifactID, nil
}

// twirpPOST issues one Twirp JSON RPC and decodes the response, mapping
// the Twirp error envelope on failure. The Bearer token lives only in the
// request header.
func (p *Provider) twirpPOST(ctx context.Context, baseURL, method, token string, reqBody, respBody any) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/"+method, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return twirpError(method, resp.StatusCode, body)
	}

	if err := json.Unmarshal(body, respBody); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}

	return nil
}

// twirpError turns a non-200 Twirp response into a classified error,
// surfacing the {code,msg} envelope when present.
func twirpError(method string, status int, body []byte) error {
	cls := errs.FromHTTPStatus(status)
	if cls == nil {
		cls = errs.ErrDependencyUnavailable
	}

	var env struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}

	if json.Unmarshal(body, &env) == nil && (env.Code != "" || env.Msg != "") {
		return fmt.Errorf("%s failed (%s): %s: %w", method, env.Code, env.Msg, cls)
	}

	return fmt.Errorf("%s: HTTP %d: %w", method, status, cls)
}

// uploadZipBlob PUTs the zip to the Azure SAS URL as a single block blob.
func (p *Provider) uploadZipBlob(ctx context.Context, signedURL string, archive zipArchive) error {
	file, err := os.Open(archive.path) //nolint:gosec // our own temp zip.
	if err != nil {
		return fmt.Errorf("open zip for upload: %w", err)
	}

	defer func() { _ = file.Close() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signedURL, file)
	if err != nil {
		return fmt.Errorf("build blob upload request: %w", err)
	}

	req.ContentLength = archive.zipSize
	req.Header.Set("x-ms-blob-type", "BlockBlob")
	req.Header.Set("x-ms-version", azureBlobAPIVersion)
	req.Header.Set("Content-Type", "application/zip")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("upload artifact blob: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return fmt.Errorf("upload artifact blob: HTTP %d: %w", resp.StatusCode, cls)
	}

	return nil
}
