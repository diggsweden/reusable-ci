// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
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
// run in.RunID into in.Dir. The repository defaults to
// $GITHUB_REPOSITORY when in.Repository is empty.
//
// Semantics match `gh run download --name <Name> --dir <Dir>`: the
// artifact's ZIP archive is extracted into in.Dir, preserving the
// archive's internal directory structure.
//nolint:cyclop // REST flow: list-by-name → choose latest matching → download → unzip → write.
func (a ArtifactDownloader) DownloadArtifact(ctx context.Context, in domainrelease.ArtifactDownloadInput) error {
	if a.Provider == nil {
		return fmt.Errorf("ArtifactDownloader requires a Provider: %w", errs.ErrUsage)
	}

	if in.RunID == "" || in.Name == "" || in.Dir == "" {
		return fmt.Errorf("ArtifactDownloader: run-id, name and dir are required: %w", errs.ErrUsage)
	}

	runID, err := strconv.ParseInt(strings.TrimSpace(in.RunID), 10, 64)
	if err != nil {
		return fmt.Errorf("parse run-id %q: %w", in.RunID, err)
	}

	owner, repo, err := resolveOwnerRepo(a.Provider, in.Repository)
	if err != nil {
		return err
	}

	client, err := a.Provider.releaseClient(ctx)
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	artifactID, err := findArtifactID(ctx, client, owner, repo, runID, in.Name)
	if err != nil {
		return err
	}

	downloadURL, _, err := client.Actions.DownloadArtifact(ctx, owner, repo, artifactID, maxArtifactRedirects)
	if err != nil {
		return fmt.Errorf("get artifact download URL: %w", err)
	}

	if err := os.MkdirAll(in.Dir, 0o755); err != nil { //nolint:gosec // artefact dir read by downstream workflow steps.
		return fmt.Errorf("mkdir %q: %w", in.Dir, err)
	}

	return downloadAndExtractZip(ctx, a.Provider.httpClient(), downloadURL.String(), in.Dir)
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
func downloadAndExtractZip(ctx context.Context, client *http.Client, url, dir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		cls := errs.FromHTTPStatus(resp.StatusCode)
		if cls == nil {
			cls = errs.ErrDependencyUnavailable
		}

		return fmt.Errorf("download artifact: HTTP %d: %w", resp.StatusCode, cls)
	}

	tmp, err := os.CreateTemp("", "reusable-ci-artifact-*.zip")
	if err != nil {
		return fmt.Errorf("create temp zip: %w", err)
	}

	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	size, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return fmt.Errorf("write temp zip: %w", err)
	}

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("open downloaded zip (%d bytes): %w", size, err)
	}

	defer func() { _ = zr.Close() }()

	return extractZipInto(zr, dir)
}

// extractZipInto writes each entry from zr under dir. Symlinks are
// rejected (gh CLI doesn't emit them; safer to refuse than to follow).
// Zip-slip is prevented by rejecting any entry whose cleaned path
// escapes dir.
func extractZipInto(zr *zip.ReadCloser, dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", dir, err)
	}

	for _, f := range zr.File { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		dest := filepath.Join(root, f.Name) //nolint:gosec // escape-check below.
		if !strings.HasPrefix(dest, root+string(os.PathSeparator)) && dest != root {
			return fmt.Errorf("zip entry %q escapes destination: %w", f.Name, errs.ErrValidation)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, f.Mode()|0o700); err != nil {
				return fmt.Errorf("mkdir %q: %w", dest, err)
			}

			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // artefact dirs read by build steps.
			return fmt.Errorf("mkdir parent of %q: %w", dest, err)
		}

		if err := writeZipFile(f, dest); err != nil {
			return err
		}
	}

	return nil
}

func writeZipFile(f *zip.File, dest string) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()|0o600) //nolint:gosec // dest path is escape-checked by caller.
	if err != nil {
		return fmt.Errorf("create %q: %w", dest, err)
	}
	// Bound per-file size so a malicious archive can't fill the disk
	// silently. 5 GiB is well above any legitimate CI artifact.
	const maxFileBytes = 5 << 30
	if _, err := io.CopyN(out, in, maxFileBytes); err != nil && !errors.Is(err, io.EOF) {
		_ = out.Close()

		return fmt.Errorf("extract %q: %w", dest, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close %q: %w", dest, err)
	}

	return nil
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
