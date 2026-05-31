// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
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

	for _, art := range matches {
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

		url, _, derr := client.Actions.DownloadArtifact(ctx, owner, repo, art.id, maxArtifactRedirects)
		if derr != nil {
			return provider.RunArtifactInfo{}, fmt.Errorf("get download URL for %q: %w", art.name, derr)
		}

		if mkErr := os.MkdirAll(dest, 0o755); mkErr != nil { //nolint:gosec // artefact dir read by downstream steps.
			return provider.RunArtifactInfo{}, fmt.Errorf("mkdir %q: %w", dest, mkErr)
		}

		bytesWritten, count, xerr := downloadAndExtractZip(ctx, p.httpClient(), url.String(), dest)
		if xerr != nil {
			return provider.RunArtifactInfo{}, xerr
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

	downloadURL, _, err := client.Actions.DownloadArtifact(ctx, owner, repo, artifactID, maxArtifactRedirects)
	if err != nil {
		return provider.RunArtifactInfo{}, fmt.Errorf("get artifact download URL: %w", err)
	}

	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil { //nolint:gosec // artefact dir read by downstream workflow steps.
		return provider.RunArtifactInfo{}, fmt.Errorf("mkdir %q: %w", dir, mkErr)
	}

	bytesWritten, count, err := downloadAndExtractZip(ctx, p.httpClient(), downloadURL.String(), dir)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	return provider.RunArtifactInfo{Name: name, Bytes: bytesWritten, FileCount: count}, nil
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
	var matches []artifactRef

	opts := &gogithub.ListOptions{PerPage: 100}

	for {
		list, resp, err := client.Actions.ListWorkflowRunArtifacts(ctx, owner, repo, runID, opts)
		if err != nil {
			return nil, fmt.Errorf("list run %d artifacts: %w", runID, err)
		}

		for _, art := range list.Artifacts {
			ok, merr := path.Match(pattern, art.GetName())
			if merr != nil {
				return nil, fmt.Errorf("invalid artifact pattern %q: %w", pattern, errs.ErrUsage)
			}

			if ok {
				matches = append(matches, artifactRef{name: art.GetName(), id: art.GetID()})
			}
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return matches, nil
}

// findArtifactID iterates the paginated artifact list for the run,
// matching by exact name. The list is small (≤ tens of artifacts per
// run) but pagination is still honoured so it works on extreme outliers.
func findArtifactID(ctx context.Context, client *gogithub.Client, owner, repo string, runID int64, name string) (int64, error) {
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		list, resp, err := client.Actions.ListWorkflowRunArtifacts(ctx, owner, repo, runID, opts)
		if err != nil {
			return 0, fmt.Errorf("list run %d artifacts: %w", runID, err)
		}

		for _, a := range list.Artifacts {
			if a.GetName() == name {
				return a.GetID(), nil
			}
		}

		if resp == nil || resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return 0, fmt.Errorf("artifact %q not found on run %d: %w", name, runID, errs.ErrReleaseNotFound)
}

// downloadAndExtractZip GETs the presigned URL and unzips into dir.
// The body is streamed to a temp file so the ZIP reader (which needs
// io.ReaderAt) doesn't materialise the whole archive in memory.
func downloadAndExtractZip(ctx context.Context, client *http.Client, url, dir string) (int64, int, error) {
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

	tmp, err := os.CreateTemp("", "reusable-ci-artifact-*.zip")
	if err != nil {
		return 0, 0, fmt.Errorf("create temp zip: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	size, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return 0, 0, fmt.Errorf("write temp zip: %w", err)
	}

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open downloaded zip (%d bytes): %w", size, err)
	}

	defer func() { _ = zr.Close() }()

	return extractZipInto(zr, dir)
}

// extractZipInto writes each entry from zr under dir, returning the total
// bytes and file count. Path safety (traversal, escape, backslash, abs) is
// the shared domain/artifact.SafeJoin guard — the same one the Forgejo
// per-file path uses. Symlink entries are rejected outright.
func extractZipInto(zr *zip.ReadCloser, dir string) (int64, int, error) {
	var (
		totalBytes int64
		count      int
	)

	for _, f := range zr.File { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if f.Mode()&fs.ModeSymlink != 0 {
			return 0, 0, fmt.Errorf("zip entry %q is a symlink: %w", f.Name, errs.ErrValidation)
		}

		dest, err := domainartifact.SafeJoin(dir, f.Name)
		if err != nil {
			return 0, 0, err
		}

		if f.FileInfo().IsDir() {
			if mkErr := os.MkdirAll(dest, f.Mode()|0o700); mkErr != nil {
				return 0, 0, fmt.Errorf("mkdir %q: %w", dest, mkErr)
			}

			continue
		}

		if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil { //nolint:gosec // artefact dirs read by build steps.
			return 0, 0, fmt.Errorf("mkdir parent of %q: %w", dest, mkErr)
		}

		n, err := writeZipFile(f, dest)
		if err != nil {
			return 0, 0, err
		}

		totalBytes += n
		count++
	}

	return totalBytes, count, nil
}

func writeZipFile(f *zip.File, dest string) (int64, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()|0o600) //nolint:gosec // dest path is SafeJoin-checked by caller.
	if err != nil {
		return 0, fmt.Errorf("create %q: %w", dest, err)
	}

	written, copyErr := io.CopyN(out, in, domainartifact.MaxFileBytes)
	closeErr := out.Close()

	switch {
	case copyErr != nil && !errors.Is(copyErr, io.EOF):
		return 0, fmt.Errorf("extract %q: %w", dest, copyErr)
	case closeErr != nil:
		return 0, fmt.Errorf("close %q: %w", dest, closeErr)
	}

	return written, nil
}

// resolveOwnerRepo splits "owner/repo" or falls back to GITHUB_REPOSITORY
// via the Provider's env function.
func resolveOwnerRepo(p *Provider, fromInput string) (string, string, error) {
	repo := strings.TrimSpace(fromInput)
	if repo == "" {
		repo = p.envFunc()("GITHUB_REPOSITORY")
	}

	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("repository must be owner/repo, got %q: %w", repo, errs.ErrUsage)
	}

	return owner, name, nil
}
