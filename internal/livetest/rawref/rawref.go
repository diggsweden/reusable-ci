// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package rawref reads forge state straight from the HTTP API, with no part of
// this repository's provider adapters involved. It is the live tier's oracle:
// the independent observation a scenario checks the product against.
//
// # Why it is its own package
//
// The entire value here is sharing nothing with the thing under test. If the
// oracle called the adapter, an adapter that mis-parsed a response would agree
// with itself and the scenario would pass while the forge held something else
// entirely. Living in a separate package lets .golangci.yml forbid importing
// internal/adapters/... here, so the independence is a build failure rather
// than a habit that erodes.
//
// It deliberately does not import internal/domain either. The oracle speaks
// wire types, so a change to the product's own vocabulary cannot silently
// reshape what "the forge says" means.
package rawref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"
)

// ErrForge is the sentinel every failure here wraps. The oracle keeps its own
// rather than reusing the product's: depguard forbids importing internal/domain
// precisely so a change there cannot reshape what this package reports.
var ErrForge = errors.New("forge read failed")

// Forge names the API dialect to speak. These are wire-level names kept as
// plain strings so this package depends on nothing of the product's.
const (
	Forgejo = "forgejo"
	GitLab  = "gitlab"
)

// Reader reads one forge instance as one identity.
type Reader struct {
	Forge string
	Base  string // https origin, no trailing slash
	Token string
	Owner string

	// Client overrides the default. TLS trust comes from the system pool,
	// where the lab CA is installed.
	Client *http.Client
}

// Release is what a forge reports about a release, reduced to the fields whose
// equality across forges is the parity claim.
type Release struct {
	Tag    string
	Name   string
	Body   string
	Assets []Asset
}

// Asset is one attached file. Digest is the SHA-256 of the bytes the forge
// actually serves — the only asset assertion that means anything, since a
// forge can report a name and a size for a file it corrupted.
type Asset struct {
	Name   string
	Size   int64
	Digest string
}

// AssetNames returns the attached names, sorted, for set comparisons.
func (r Release) AssetNames() []string {
	names := make([]string, 0, len(r.Assets))
	for _, asset := range r.Assets {
		names = append(names, asset.Name)
	}

	sort.Strings(names)

	return names
}

// Asset finds an attached asset by name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}

	return Asset{}, false
}

func (r Reader) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return &http.Client{Timeout: 60 * time.Second}
}

// ReleaseByTag reads a release and downloads every asset to digest it.
// A missing release is (Release{}, false, nil): absence is an answer a scenario
// asserts on, not an error.
func ReleaseByTag(ctx context.Context, reader Reader, repo, tag string) (Release, bool, error) {
	switch reader.Forge {
	case Forgejo:
		return reader.forgejoRelease(ctx, repo, tag)
	case GitLab:
		return reader.gitLabRelease(ctx, repo, tag)
	default:
		return Release{}, false, fmt.Errorf("rawref: no reader for forge %q: %w", reader.Forge, ErrForge)
	}
}

func (r Reader) forgejoRelease(ctx context.Context, repo, tag string) (Release, bool, error) {
	endpoint := r.Base + "/api/v1/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(repo) +
		"/releases/tags/" + url.PathEscape(tag)

	var payload struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}

	found, err := r.get(ctx, endpoint, &payload)
	if err != nil || !found {
		return Release{}, false, err
	}

	release := Release{Tag: payload.TagName, Name: payload.Name, Body: payload.Body}

	for _, asset := range payload.Assets {
		digest, digestErr := r.digest(ctx, asset.URL)
		if digestErr != nil {
			return Release{}, false, digestErr
		}

		release.Assets = append(release.Assets, Asset{Name: asset.Name, Size: asset.Size, Digest: digest})
	}

	return release, true, nil
}

func (r Reader) gitLabRelease(ctx context.Context, repo, tag string) (Release, bool, error) {
	project := url.PathEscape(r.Owner + "/" + repo)
	endpoint := r.Base + "/api/v4/projects/" + project + "/releases/" + url.PathEscape(tag)

	// GitLab models attachments as generic-package links rather than files on
	// the release, so the shapes genuinely differ here. That difference is the
	// adapter's to hide; the oracle just reads each forge as it is.
	var payload struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Assets      struct {
			Links []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"links"`
		} `json:"assets"`
	}

	found, err := r.get(ctx, endpoint, &payload)
	if err != nil || !found {
		return Release{}, false, err
	}

	release := Release{Tag: payload.TagName, Name: payload.Name, Body: payload.Description}

	for _, link := range payload.Assets.Links {
		digest, size, digestErr := r.digestAndSize(ctx, link.URL)
		if digestErr != nil {
			return Release{}, false, digestErr
		}

		release.Assets = append(release.Assets, Asset{Name: link.Name, Size: size, Digest: digest})
	}

	return release, true, nil
}

func (r Reader) get(ctx context.Context, endpoint string, into any) (bool, error) {
	resp, err := r.do(ctx, endpoint)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("rawref: GET %s: HTTP %d: %w", redact(endpoint), resp.StatusCode, ErrForge)
	}

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return false, fmt.Errorf("rawref: decode %s: %w", redact(endpoint), err)
	}

	return true, nil
}

func (r Reader) digest(ctx context.Context, rawURL string) (string, error) {
	digest, _, err := r.digestAndSize(ctx, rawURL)

	return digest, err
}

// digestAndSize streams an asset and hashes it. The bytes are never held whole:
// an asset is whatever size the scenario attached, and a test helper that can
// be made to allocate it is a denial of service against the developer.
func (r Reader) digestAndSize(ctx context.Context, rawURL string) (string, int64, error) {
	resp, err := r.do(ctx, rawURL)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("rawref: download %s: HTTP %d: %w", redact(rawURL), resp.StatusCode, ErrForge)
	}

	hash := sha256.New()

	size, err := io.Copy(hash, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("rawref: read %s: %w", redact(rawURL), err)
	}

	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func (r Reader) do(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("rawref: build request: %w", err)
	}

	switch r.Forge {
	case Forgejo:
		req.Header.Set("Authorization", "token "+r.Token)
	case GitLab:
		req.Header.Set("PRIVATE-TOKEN", r.Token)
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("rawref: GET %s: %w", redact(endpoint), err)
	}

	return resp, nil
}

// SHA256 digests a local file, so a scenario can state the digest it uploaded
// without recomputing it by hand.
func SHA256(path string) (string, int64, error) {
	// G304: path comes from the scenario's own t.TempDir(), never from input.
	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		return "", 0, fmt.Errorf("rawref: open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()

	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("rawref: read %s: %w", path, err)
	}

	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// FormatSize renders a byte count for a failure message.
func FormatSize(n int64) string { return strconv.FormatInt(n, 10) + "B" }

func redact(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "<unparseable endpoint>"
	}

	parsed.RawQuery = ""

	return parsed.String()
}
