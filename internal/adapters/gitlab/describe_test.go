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

	if !caps.ReleaseAssets || !caps.KeylessOIDC {
		t.Errorf("Capabilities = %+v, want ReleaseAssets + KeylessOIDC", caps)
	}
}
