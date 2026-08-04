// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
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

func TestCapabilities_GitLab(t *testing.T) {
	t.Parallel()

	caps := gitlab.New().Capabilities()
	if caps.SARIFUpload {
		t.Error("GitLab should not advertise SARIFUpload")
	}

	if !caps.ReleaseAssets {
		t.Error("GitLab should advertise ReleaseAssets")
	}

	if !caps.PublicFulcioTrusted {
		t.Errorf("Capabilities = %+v, want PublicFulcioTrusted", caps)
	}

	// Run artifacts are omitted by design: GitLab passes them declaratively
	// via artifacts:/needs: in the job template, not this binary.
	if caps.RunArtifacts {
		t.Error("GitLab must not advertise RunArtifacts (declarative artifacts:/needs:)")
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
