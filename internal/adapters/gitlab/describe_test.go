// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestDescribe_GitLab_SaaSDefault(t *testing.T) {
	t.Parallel()

	// Empty CI_SERVER_URL → gitlab.com SaaS default. Inject via the
	// adapter's Env field so the test stays hermetic.
	p := &gitlab.Provider{Env: func(string) string { return "" }}

	info := p.Describe()
	if info.DisplayName != "GitLab" {
		t.Errorf("DisplayName = %q, want GitLab", info.DisplayName)
	}

	if info.OIDCIssuer != "https://gitlab.com" {
		t.Errorf("OIDCIssuer = %q, want https://gitlab.com", info.OIDCIssuer)
	}
}

func TestDescribe_GitLab_SelfHostedIssuer(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		if k == "CI_SERVER_URL" {
			return "https://gitlab.diggsweden.internal"
		}

		return ""
	}}

	if got := p.Describe().OIDCIssuer; got != "https://gitlab.diggsweden.internal" {
		t.Errorf("OIDCIssuer = %q, want CI_SERVER_URL value", got)
	}
}

// TestCapabilities_GitLab asserts the whole struct, from a fixed environment.
//
// Two things were wrong with checking four fields individually. It read
// gitlab.New(), whose Env is os.Getenv, so CI_SERVER_URL from whatever shell
// or runner the suite happened to run in decided PublicFulcioTrusted — the one
// field here that is not a constant. And a capability added to the struct
// defaults to false, so the matrix could gain a field this adapter silently
// under-reports and every individual check still passes.
//
// Exact equality fixes both: a new field forces a decision here, and the
// environment is supplied rather than inherited.
func TestCapabilities_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(string) string { return "" }}

	want := provider.Capabilities{
		// Derived from the roles this adapter implements.
		ReleaseAssets:           true,
		ContainerTagDeletion:    true,
		ContainerPackageListing: true,

		// Declared.
		MintsOIDCToken:      true,
		PublicFulcioTrusted: true,

		// False by design, each for its own reason: GitLab consumes the SAST
		// JSON report rather than SARIF, has no build-provenance attestation
		// API, and passes intra-pipeline artifacts declaratively through
		// artifacts:/needs: in the job template rather than a programmatic
		// in-job store.
		SARIFUpload:  false,
		Attestation:  false,
		RunArtifacts: false,
	}
	if got := p.Capabilities(); got != want {
		t.Errorf("Capabilities = %+v\nwant                %+v", got, want)
	}
}

// The keyless claim is per-instance, not per-forge. Public Fulcio trusts
// gitlab.com and no other GitLab, so the same adapter must answer differently
// behind a $CI_SERVER_URL — otherwise a self-hosted run is told keyless works
// out of the box, gets no warning, and fails inside cosign instead.
func TestCapabilities_GitLab_SelfHostedIsNotPublicFulcioTrusted(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		if k == "CI_SERVER_URL" {
			return "https://gitlab.diggsweden.internal"
		}

		return ""
	}}

	caps := p.Capabilities()
	if caps.PublicFulcioTrusted {
		t.Error("a self-hosted GitLab issuer is not one public Fulcio trusts")
	}

	// It can still sign keylessly — against a Fulcio told to trust it.
	if !caps.MintsOIDCToken {
		t.Error("GitLab CI mints id-tokens on every instance, self-hosted included")
	}
}
