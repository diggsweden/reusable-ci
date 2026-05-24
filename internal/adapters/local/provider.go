// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package local implements the always-available provider port for the
// dev / test loop.
//
// "Local" means: not GitHub Actions, not GitLab CI. Used when running
// the binary on a developer's machine or in a unit test that wants a
// real provider without faking one. Reads from a small set of env vars
// the developer can set; everything else returns zero values.
//
// The local adapter intentionally does NOT implement TokenValidator,
// ReleaseCreator, ReleaseAssetUploader, or SARIFUploader — those
// capabilities require a real CI platform API to be meaningful. CLI
// commands that need them gate on platform first, so the operator
// gets a clear "feature X requires GitHub/GitLab CI" at the command
// boundary rather than a runtime ErrUnsupported deep in the call
// stack.
package local

import (
	"context"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Provider satisfies the always-available provider port for ad-hoc
// local invocations.
type Provider struct {
	Env func(string) string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider { return &Provider{Env: os.Getenv} }

// Name reports the platform identifier.
func (p *Provider) Name() provider.Platform { return provider.PlatformLocal }

// ResolveContext returns an EventContext populated from the equivalent
// CI_* / git env vars when present. Empty otherwise. Tests inject fixed
// values via the Env field.
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.Env
	if get == nil {
		get = os.Getenv
	}

	sha := get("CI_COMMIT")

	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	return &provider.EventContext{
		Platform: provider.PlatformLocal,
		RefName:  get("CI_REF_NAME"),
		SHA:      sha,
		ShortSHA: short,
		Branch:   get("CI_BRANCH"),
		Repo:     get("CI_REPO"),
	}, nil
}

// FetchRepoMetadata returns empty metadata. Local mode has no API to
// query — operators that want OCI labels in dev runs supply
// OCI_DESCRIPTION / OCI_LICENSE env overrides on the use case directly.
// Empty fields are valid output under the RepoMetadataFetcher contract.
func (p *Provider) FetchRepoMetadata(_ context.Context, _ string) (*provider.RepoMetadata, error) {
	return &provider.RepoMetadata{}, nil
}

// Compile-time conformance checks. local.Provider satisfies the
// always-available base + RepoMetadataFetcher. It deliberately does
// not satisfy TokenValidator / ReleaseCreator / ReleaseAssetUploader /
// SARIFUploader — those CLI surfaces gate on platform.
var (
	_ provider.Provider            = (*Provider)(nil)
	_ provider.RepoMetadataFetcher = (*Provider)(nil)
)
