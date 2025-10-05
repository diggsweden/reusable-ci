// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// maxArtifactRedirects bounds the redirect chain the go-github client
// is allowed to follow before returning the download URL. GitHub's
// artifact-download endpoint responds with a 302 to a presigned blob
// URL; one hop is always enough.
const maxArtifactRedirects = 3

// ArtifactDownloader downloads workflow-run artifacts in-process via
// the GitHub REST API. The previous implementation shelled out to
// `gh run download`; this replaces it with go-github + archive/zip so
// there is no runtime dependency on the gh CLI.
type ArtifactDownloader struct {
	// Provider supplies the authenticated go-github client and the
	// retry-wrapped http.Client. Required.
	Provider *Provider
}

// NewArtifactDownloader returns a downloader that piggybacks on the
// given Provider's client + retry transport. Passing a nil Provider
// makes every call return an ErrUsage.
func NewArtifactDownloader(p *Provider) ArtifactDownloader {
	return ArtifactDownloader{Provider: p}
}

// DownloadArtifact downloads the artifact named in.Name from workflow
// run in.RunID into in.Dir (the transfer-plan caller). It delegates to the
// shared core; the byte/file totals are not needed on this path.
func (a ArtifactDownloader) DownloadArtifact(ctx context.Context, in domainrelease.ArtifactDownloadInput) error {
	if a.Provider == nil {
		return fmt.Errorf("ArtifactDownloader requires a Provider: %w", errs.ErrUsage)
	}

	_, err := a.Provider.downloadRunArtifact(ctx, in.RunID, in.Repository, in.Name, in.Dir)

	return err
}

// DownloadRunArtifact implements provider.RunArtifactDownloader via the
// GitHub REST artifacts API. Unlike Forgejo's run-scoped runtime token,
// GitHub can read artifacts from any run the token can see, so an explicit
// RunID (or the current run, when empty) is honored — there is no
// cross-run restriction.
func (p *Provider) DownloadRunArtifact(ctx context.Context, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	runID := in.RunID
	if runID == "" {
		runID = p.envFunc()("GITHUB_RUN_ID")
	}

	if in.Pattern != "" {
		return p.downloadMatchingArtifacts(ctx, runID, in)
	}

	if err := domainartifact.ValidateName(in.Name); err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return p.downloadRunArtifact(ctx, runID, in.Repository, in.Name, in.Dir)
}

// downloadMatchingArtifacts implements the pattern/merge-multiple download:
// list the run's artifacts, keep those whose name matches the glob, and
// download each — flattened into Dir (MergeMultiple) or into Dir/<name>/.
// Zero matches is not an error (mirrors download-artifact's pattern behaviour:
// a missing optional artifact set is a no-op, surfaced by the empty result).
//
//nolint:cyclop // linear: parse → resolve repo → client → list → filter → per-artifact extract.
func (p *Provider) downloadMatchingArtifacts(ctx context.Context, runIDStr string, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	if _, err := path.Match(in.Pattern, ""); err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("invalid artifact pattern: %w", errs.ErrUsage)
	}

	if runIDStr == "" || in.Dir == "" {
		return provider.RunArtifactInfo{}, fmt.Errorf("run-id and dir are required: %w", errs.ErrUsage)
	}

	runID, err := strconv.ParseInt(strings.TrimSpace(runIDStr), 10, 64)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("parse run-id %q: %w", runIDStr, err)
	}

	owner, repo, err := resolveOwnerRepo(p, in.Repository)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("github client: %w", err)
	}

	matches, err := listMatchingArtifacts(ctx, client, owner, repo, runID, in.Pattern)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	var totalBytes int64

	var totalFiles int

	destinations := make([]string, len(matches))
	for index, art := range matches {
		// The artifact name comes from the forge — for a pattern download it
		// may be a PR author's or another job's artifact. Validate it (control
		// chars / invalid UTF-8) and resolve the per-artifact destination via
		// SafeJoin so a hostile name can't traverse out of Dir.
		if nameErr := domainartifact.ValidateName(art.name); nameErr != nil {
			return provider.RunArtifactInfo{}, fmt.Errorf("forge returned an unsafe artifact name: %w", nameErr)
		}

		dest := in.Dir

		if !in.MergeMultiple {
			safeDest, joinErr := domainartifact.SafeJoin(in.Dir, art.name)
			if joinErr != nil {
				return provider.RunArtifactInfo{}, joinErr
			}

			dest = safeDest
		}

		checked, err := pathsafe.OpenRoot(dest)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return provider.RunArtifactInfo{}, err
		}

		if checked != nil {
			_ = checked.Close()
		}

		destinations[index] = dest
	}

	for index, art := range matches {
		url, response, derr := client.Actions.DownloadArtifact(ctx, owner, repo, art.id, maxArtifactRedirects)
		if derr != nil {
			return provider.RunArtifactInfo{}, fmt.Errorf("get download URL for %q: %w", art.name, classifyArtifactResponse(response, derr))
		}

		bytesWritten, count, xerr := downloadAndExtractZip(
			ctx,
			p.httpClient(),
			url.String(),
			destinations[index],
			domainartifact.MaxTotalBytes-totalBytes,
			domainartifact.MaxFileCount-totalFiles,
		)
		if xerr != nil {
			return provider.RunArtifactInfo{}, xerr
		}

		if totalErr := domainartifact.ValidateAggregate(totalBytes, totalFiles, bytesWritten, count); totalErr != nil {
			return provider.RunArtifactInfo{}, totalErr
		}

		totalBytes += bytesWritten
		totalFiles += count
	}

	return provider.RunArtifactInfo{Name: in.Pattern, Bytes: totalBytes, FileCount: totalFiles}, nil
}

// downloadRunArtifact is the shared REST flow: list-by-name → choose the
// matching artifact → resolve the presigned URL → unzip into dir. The ZIP
// is extracted through the shared domain/artifact safety guards.
//
//nolint:cyclop // linear REST flow: parse → resolve repo → client → find → url → mkdir → extract.
func (p *Provider) downloadRunArtifact(ctx context.Context, runIDStr, repository, name, dir string) (provider.RunArtifactInfo, error) {
	if runIDStr == "" || name == "" || dir == "" {
		return provider.RunArtifactInfo{}, fmt.Errorf("run-id, name and dir are required: %w", errs.ErrUsage)
	}

	runID, err := strconv.ParseInt(strings.TrimSpace(runIDStr), 10, 64)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("parse run-id %q: %w", runIDStr, err)
	}

	owner, repo, err := resolveOwnerRepo(p, repository)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	client, err := p.releaseClient(ctx)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("github client: %w", err)
	}

	artifactID, err := findArtifactID(ctx, client, owner, repo, runID, name)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	downloadURL, response, err := client.Actions.DownloadArtifact(ctx, owner, repo, artifactID, maxArtifactRedirects)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("get artifact download URL: %w", classifyArtifactResponse(response, err))
	}

	bytesWritten, count, err := downloadAndExtractZip(
		ctx,
		p.httpClient(),
		downloadURL.String(),
		dir,
		domainartifact.MaxTotalBytes,
		domainartifact.MaxFileCount,
	)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: name, Bytes: bytesWritten, FileCount: count}, nil
}

func classifyArtifactResponse(response *gogithub.Response, err error) error {
	// DownloadArtifact reports nonredirect HTTP errors as untyped errors.
	if response != nil {
		if class := errs.FromHTTPStatus(response.StatusCode); class != nil {
			return fmt.Errorf("GitHub artifact HTTP %d: %w", response.StatusCode, class)
		}
	}

	return classifyGitHubError(err)
}

// artifactRef is a run artifact's name and numeric id.
type artifactRef struct {
	name string
	id   int64
}

// listMatchingArtifacts returns every run artifact whose name matches the glob
// pattern. Artifact names carry no "/", so path.Match (*, ?, [set]) is the
// right matcher — and the same one download-artifact's minimatch reduces to for
// these names.
func listMatchingArtifacts(ctx context.Context, client *gogithub.Client, owner, repo string, runID int64, pattern string) ([]artifactRef, error) {
	artifacts, err := listRunArtifacts(ctx, client, owner, repo, runID)
	if err != nil {
		return nil, err
	}

	var matches []artifactRef

	for _, art := range artifacts {
		ok, merr := path.Match(pattern, art.GetName())
		if merr != nil {
			return nil, fmt.Errorf("invalid artifact pattern %q: %w", pattern, errs.ErrUsage)
		}

		if ok {
			matches = append(matches, artifactRef{name: art.GetName(), id: art.GetID()})
		}
	}

	return matches, nil
}

// listRunArtifacts returns every artifact on the run, across pages.
func listRunArtifacts(ctx context.Context, client *gogithub.Client, owner, repo string, runID int64) ([]*gogithub.Artifact, error) {
	return listPages(func(opts *gogithub.ListOptions) ([]*gogithub.Artifact, *gogithub.Response, error) {
		list, resp, err := client.Actions.ListWorkflowRunArtifacts(ctx, owner, repo, runID, opts)
		if err != nil {
			return nil, resp, fmt.Errorf("list run %d artifacts: %w", runID, classifyGitHubError(err))
		}

		return list.Artifacts, resp, nil
	})
}

// findArtifactID looks the run's artifacts up by exact name, across every
// page. The list is small (≤ tens of artifacts per run) but pagination is
// still honoured so it works on extreme outliers, and a name on two pages is
// as ambiguous as a name twice on one.
func findArtifactID(ctx context.Context, client *gogithub.Client, owner, repo string, runID int64, name string) (int64, error) {
	artifacts, err := listRunArtifacts(ctx, client, owner, repo, runID)
	if err != nil {
		return 0, err
	}

	var matched *gogithub.Artifact

	for _, candidate := range artifacts {
		if candidate.GetName() != name {
			continue
		}

		if matched != nil {
			return 0, fmt.Errorf("multiple artifacts named %q on run %d: %w", name, runID, errs.ErrValidation)
		}

		matched = candidate
	}

	if matched == nil {
		return 0, fmt.Errorf("artifact %q not found on run %d: %w", name, runID, errs.ErrReleaseNotFound)
	}

	if matched.GetID() <= 0 {
		return 0, fmt.Errorf("matching artifact has no valid id: %w", errs.ErrMalformedInput)
	}

	return matched.GetID(), nil
}

// downloadAndExtractZip GETs the presigned URL and unzips into dir.
// The body is streamed to a temp file so the ZIP reader (which needs
// io.ReaderAt) doesn't materialise the whole archive in memory.
//
//nolint:cyclop // linear response validation, bounded spool, then extraction.
func downloadAndExtractZip(ctx context.Context, client *http.Client, url, dir string, maxBytes int64, maxFiles int) (int64, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("download artifact: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return 0, 0, fmt.Errorf("download artifact: HTTP %d: %w", resp.StatusCode, cls)
	}

	if resp.ContentLength > domainartifact.MaxArchiveBytes {
		return 0, 0, fmt.Errorf("download artifact body exceeds %d bytes: %w", domainartifact.MaxArchiveBytes, errs.ErrValidation)
	}

	tmp, err := os.CreateTemp("", "reusable-ci-artifact-*.zip")
	if err != nil {
		return 0, 0, fmt.Errorf("create temp zip: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	size, err := domainartifact.CopyAtMost(tmp, resp.Body, domainartifact.MaxArchiveBytes)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return 0, 0, fmt.Errorf("write temp zip: %w", err)
	}

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		// Classified, because an unclassified error here exits 70, which tells
		// the operator this program is at fault — for a body the forge served
		// that is not an archive. The download reached a server and the server
		// answered; what came back was wrong, which is the same shape as any
		// other refusal and is the operator's to investigate, not ours.
		return 0, 0, fmt.Errorf("open downloaded zip (%d bytes): %w: %w", size, err, errs.ErrValidation)
	}

	defer func() { _ = zr.Close() }()

	stage, err := pathsafe.NewArtifactStaging(dir)
	if err != nil {
		return 0, 0, err
	}

	defer func() { _ = stage.Close() }()

	bytesWritten, count, err := extractZipInto(zr, stage.Root(), maxBytes, maxFiles)
	if err != nil {
		return 0, 0, err
	}

	if err := stage.Install(); err != nil {
		return 0, 0, err
	}

	return bytesWritten, count, nil
}

// extractZipInto writes each entry from zr under root, returning the total
// bytes and file count. Path safety (traversal, escape, backslash, abs) is
// the shared domain/artifact.SafeJoin guard — the same one the Forgejo
// per-file path uses. Symlink entries are rejected outright.
//
//nolint:cyclop // each branch is a distinct archive-entry safety check.
func extractZipInto(zr *zip.ReadCloser, root *os.Root, maxBytes int64, maxFiles int) (int64, int, error) {
	var (
		totalBytes int64
		count      int
		entries    int
	)

	for _, f := range zr.File { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		entries++
		if entries > domainartifact.MaxFileCount {
			return 0, 0, fmt.Errorf("zip exceeds %d entries: %w", domainartifact.MaxFileCount, errs.ErrValidation)
		}

		if f.Mode()&fs.ModeSymlink != 0 {
			return 0, 0, fmt.Errorf("zip entry %q is a symlink: %w", f.Name, errs.ErrValidation)
		}

		_, err := domainartifact.SafeJoin(".", f.Name)
		if err != nil {
			return 0, 0, err
		}

		dest := filepath.FromSlash(f.Name)

		if f.FileInfo().IsDir() {
			if mkErr := root.MkdirAll(dest, f.Mode()|0o700); mkErr != nil {
				return 0, 0, fmt.Errorf("mkdir %q: %w", dest, mkErr)
			}

			continue
		}

		if count >= maxFiles {
			return 0, 0, fmt.Errorf("zip exceeds remaining artifact file budget: %w", errs.ErrValidation)
		}

		remainingBytes := maxBytes - totalBytes
		if remainingBytes < 0 {
			return 0, 0, fmt.Errorf("zip exceeds remaining artifact byte budget: %w", errs.ErrValidation)
		}

		remainingLimit := uint64(remainingBytes) //nolint:gosec // non-negative check above makes the conversion exact.
		if f.UncompressedSize64 > uint64(domainartifact.MaxFileBytes) || f.UncompressedSize64 > remainingLimit {
			return 0, 0, fmt.Errorf("zip entry %q exceeds extraction limits: %w", f.Name, errs.ErrValidation)
		}

		declaredBytes := int64(f.UncompressedSize64) //nolint:gosec // bounds above prove the value fits in int64.
		if totalErr := domainartifact.ValidateTotals(totalBytes, count, declaredBytes); totalErr != nil {
			return 0, 0, fmt.Errorf("zip entry %q: %w", f.Name, totalErr)
		}

		if mkErr := root.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil { //nolint:gosec // os.Root contains artifact dirs.
			return 0, 0, fmt.Errorf("mkdir parent of %q: %w", dest, mkErr)
		}

		limit := min(domainartifact.MaxFileBytes, maxBytes-totalBytes)

		n, err := writeZipFile(root, f, dest, limit)
		if err != nil {
			return 0, 0, err
		}

		totalBytes += n
		count++
	}

	return totalBytes, count, nil
}

func writeZipFile(root *os.Root, f *zip.File, dest string, maxBytes int64) (int64, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}

	defer func() { _ = in.Close() }()

	if removeErr := root.Remove(dest); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		return 0, fmt.Errorf("replace %q: %w", dest, removeErr)
	}

	out, err := root.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode()|0o600) //nolint:gosec // os.Root contains dest.
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", dest, err)
	}

	complete := false
	defer func() {
		if !complete {
			_ = out.Close()
			_ = root.Remove(dest)
		}
	}()

	written, copyErr := domainartifact.CopyAtMost(out, in, maxBytes)
	closeErr := out.Close()

	switch {
	case copyErr != nil:
		return 0, fmt.Errorf("extract %q: %w", dest, copyErr)
	case closeErr != nil:
		return 0, fmt.Errorf("close %q: %w", dest, closeErr)
	}

	complete = true

	return written, nil
}

// resolveOwnerRepo splits "owner/repo" or falls back to GITHUB_REPOSITORY
// via the Provider's env function.
func resolveOwnerRepo(p *Provider, fromInput string) (string, string, error) {
	repo := fromInput
	if repo == "" {
		repo = p.envFunc()("GITHUB_REPOSITORY")
	}

	return splitRepo(repo)
}
