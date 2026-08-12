// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package local implements the always-available provider port for the
// dev / test loop.
//
// "Local" means: not GitHub Actions, not GitLab CI. Used when running
// the binary on a developer's machine or in a unit test that wants a
// real provider without faking one. Reads from a small set of env vars
// the developer can set; everything else returns zero values.
//
// The local adapter intentionally does NOT implement TokenValidator,
// ReleaseCreator, ReleasePublisher, ReleaseAssetUploader, or SARIFUploader — those
// capabilities require a real CI platform API to be meaningful. CLI
// commands that need them gate on platform first, so the operator
// gets a clear "feature X requires GitHub/GitLab CI" at the command
// boundary rather than a runtime ErrUnsupported deep in the call
// stack.
package local

import (
	"context"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// Provider satisfies the always-available provider port for ad-hoc
// local invocations.
type Provider struct {
	Env func(string) string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider { return &Provider{Env: os.Getenv} }

// Name reports the platform identifier.
func (p *Provider) Name() provider.ForgeAPI { return provider.ForgeLocal }

// ResolveContext returns an EventContext populated from the run-context env
// vars when present, and empty otherwise. Tests inject fixed values via the
// Env field.
//
// The names come from the runcontext chains, not from a list kept here. Reading
// one hand-picked name per concept drifts: the chains lead with the
// forge-neutral $REPOSITORY / $REF_NAME that `release publish` honours, so a
// developer exporting those would have been met with an empty context in local
// mode alone.
func (p *Provider) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	get := p.Env
	if get == nil {
		get = os.Getenv
	}

	sha := runcontext.Commit().Resolve(get)

	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	return &provider.EventContext{
		ForgeAPI: provider.ForgeLocal,
		RefName:  runcontext.RefName().Resolve(get),
		SHA:      sha,
		ShortSHA: short,
		// No branch chain exists: $CI_BRANCH is the name `report lifecycle`
		// already documents for the same concept, so it is read directly rather
		// than inventing a chain for one consumer.
		Branch: get("CI_BRANCH"),
		Repo:   runcontext.Repository().Resolve(get),
	}, nil
}

// FetchRepoMetadata returns empty metadata. Local mode has no API to
// query — operators that want OCI labels in dev runs supply
// OCI_DESCRIPTION / OCI_LICENSE env overrides on the use case directly.
// Empty fields are valid output under the RepoMetadataFetcher contract.
func (p *Provider) FetchRepoMetadata(_ context.Context, _ string) (*provider.RepoMetadata, error) {
	return &provider.RepoMetadata{}, nil
}

// Describe returns generic local self-description. No setup URL or OIDC
// issuer is known — operators supply tokens and --oidc-issuer
// explicitly in local mode.
func (p *Provider) Describe() provider.Info {
	return provider.Info{
		DisplayName: "local",
		ScopesHint:  "Provide a token with the appropriate permissions.",
	}
}

// Capabilities reports no forge features: local mode implements none of the
// capability roles (there is no API to call) and declares nothing, so
// derivation yields all-false. There is no runner here to mint an id-token, so
// both keyless capabilities are false — signing on a laptop uses a key.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.DeriveCapabilities(p, provider.Declared{})
}

// Compile-time conformance checks. local.Provider satisfies the
// always-available base + RepoMetadataFetcher + self-description roles.
// It deliberately does not satisfy TokenValidator / ReleaseCreator /
// ReleasePublisher / ReleaseAssetUploader / SARIFUploader — those CLI surfaces gate on
// platform.
var (
	_ provider.Provider            = (*Provider)(nil)
	_ provider.RepoMetadataFetcher = (*Provider)(nil)
	_ provider.Describer           = (*Provider)(nil)
	_ provider.CapabilityReporter  = (*Provider)(nil)
)
