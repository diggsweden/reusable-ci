// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestForgeAPI_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given provider.ForgeAPI
		want  bool
	}{
		{"github_is_valid", provider.ForgeGitHub, true},
		{"gitlab_is_valid", provider.ForgeGitLab, true},
		{"local_is_valid", provider.ForgeLocal, true},
		{"empty_is_invalid", provider.ForgeAPI(""), false},
		{"unknown_provider_is_invalid", provider.ForgeAPI("bitbucket"), false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, testCase.given.IsValid())
		})
	}
}

func TestForgeAPI_String_GitHubIsLowerCase(t *testing.T) {
	t.Parallel()
	require.Equal(t, "github", provider.ForgeGitHub.String())
}

// uploadOnly implements half of the run-artifact pair; runArtifactStore the
// whole of it. Neither touches a network.
type uploadOnly struct{}

func (uploadOnly) UploadRunArtifact(context.Context, provider.RunArtifactUpload) (provider.RunArtifactInfo, error) {
	return provider.RunArtifactInfo{}, nil
}

type runArtifactStore struct{ uploadOnly }

func (runArtifactStore) DownloadRunArtifact(context.Context, provider.RunArtifactDownload) (provider.RunArtifactInfo, error) {
	return provider.RunArtifactInfo{}, nil
}

// TestDeriveCapabilities_RolesAndDeclarations pins the two rules the bools
// depend on: a half-implemented run-artifact pair promises no store, since it
// cannot round-trip, and a declared public-Fulcio trust implies the wider
// claim that the forge mints OIDC tokens at all.
func TestDeriveCapabilities_RolesAndDeclarations(t *testing.T) {
	t.Parallel()

	require.False(t, provider.DeriveCapabilities(uploadOnly{}, provider.Declared{}).RunArtifacts, "an uploader without a downloader reported a run-artifact store")
	require.True(t, provider.DeriveCapabilities(runArtifactStore{}, provider.Declared{}).RunArtifacts)

	bare := provider.DeriveCapabilities(struct{}{}, provider.Declared{})
	require.Equal(t, provider.Capabilities{}, bare, "a value implementing no role reported a capability")

	trusted := provider.DeriveCapabilities(struct{}{}, provider.Declared{PublicFulcioTrusted: true, Attestation: true})
	require.True(t, trusted.MintsOIDCToken, "public Fulcio trust did not imply OIDC minting")
	require.True(t, trusted.PublicFulcioTrusted)
	require.True(t, trusted.Attestation)

	minting := provider.DeriveCapabilities(struct{}{}, provider.Declared{MintsOIDCToken: true})
	require.True(t, minting.MintsOIDCToken)
	require.False(t, minting.PublicFulcioTrusted, "minting alone claimed public Fulcio trust")
}

// TestPublicFulcioTrusts_OnlyTheTwoOnboardedIssuers: public Fulcio matches
// issuers by exact URL, so only github.com's Actions issuer and gitlab.com
// qualify; a trailing slash or surrounding whitespace is normalised, and a
// self-hosted instance of the same software is not on the list.
func TestPublicFulcioTrusts_OnlyTheTwoOnboardedIssuers(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		issuer string
		want   bool
	}{
		{"https://token.actions.githubusercontent.com", true},
		{"https://token.actions.githubusercontent.com/", true},
		{" https://gitlab.com/ ", true},
		{"https://gitlab.example.org", false},
		{"https://github.example.org/_services/token", false},
		{"https://forgejo.example.org", false},
		{"http://token.actions.githubusercontent.com", false},
		{"https://token.actions.githubusercontent.com/x", false},
		{"", false},
	} {
		require.Equal(t, testCase.want, provider.PublicFulcioTrusts(testCase.issuer), "issuer %q", testCase.issuer)
	}
}
