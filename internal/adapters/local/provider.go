// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package local implements provider.Provider for the dev / test loop.
//
// "Local" means: not GitHub Actions, not GitLab CI. Used when running
// the binary on a developer's machine or in a unit test that wants a
// real Provider without faking one. Reads from a small set of env vars
// the developer can set; everything else returns zero values.
package local

import (
	"context"
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Provider satisfies provider.Provider for ad-hoc local invocations.
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
// query — operators that want OCI labels in dev runs supply OCI_DESCRIPTION
// / OCI_LICENSE env overrides on the use case directly.
func (p *Provider) FetchRepoMetadata(_ context.Context, _ string) (*provider.RepoMetadata, error) {
	return &provider.RepoMetadata{}, nil
}

// ValidateToken returns an error in local mode — token validation has
// no meaning without a real platform API to probe.
func (p *Provider) ValidateToken(_ context.Context, _, _ string) error {
	return fmt.Errorf("token validation requires GitHub Actions or GitLab CI (got local): %w", errs.ErrUnsupported)
}

// ValidateBotPermissions returns a zero BotPermissions in local mode.
// Use cases that surface this normally fail validation upstream by
// checking Provider.Name() first.
func (p *Provider) ValidateBotPermissions(_ context.Context, _ string) (*provider.BotPermissions, error) {
	return &provider.BotPermissions{}, nil
}

// CreateRelease is unsupported in local mode — there's no platform to
// create a release on.
func (p *Provider) CreateRelease(_ context.Context, _ string, _ provider.ReleaseSpec) error {
	return fmt.Errorf("release creation requires GitHub Actions or GitLab CI (got local): %w", errs.ErrUnsupported)
}

// UploadSARIF is unsupported in local mode — GitHub Code Scanning is
// the only consumer this method addresses.
func (p *Provider) UploadSARIF(_ context.Context, _ provider.SARIFUpload) error {
	return fmt.Errorf("SARIF upload requires GitHub Actions (got local): %w", errs.ErrUnsupported)
}

// Compile-time conformance check.
var _ provider.Provider = (*Provider)(nil)
