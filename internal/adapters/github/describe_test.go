// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
)

func TestDescribe_GitHub(t *testing.T) {
	t.Parallel()

	info := github.New().Describe()
	if info.DisplayName != "GitHub" {
		t.Errorf("DisplayName = %q, want GitHub", info.DisplayName)
	}

	if info.OIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("OIDCIssuer = %q", info.OIDCIssuer)
	}

	if !strings.Contains(info.ScopesHint, "fine-grained PAT") {
		t.Errorf("ScopesHint = %q", info.ScopesHint)
	}

	if !strings.Contains(info.SetupURL, "personal-access-tokens") {
		t.Errorf("SetupURL = %q", info.SetupURL)
	}
}

func TestCapabilities_GitHub(t *testing.T) {
	t.Parallel()

	caps := github.New().Capabilities()
	if !caps.SARIFUpload || !caps.Attestation || !caps.PublicFulcioTrusted || !caps.ReleaseAssets || !caps.RunArtifacts {
		t.Errorf("Capabilities = %+v, want all true", caps)
	}
}

func TestAdviseToken_GitHub(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		token        string
		wantReject   bool
		wantContains string
	}{
		{"classic_refused", "ghp_classic", true, "classic PAT detected"},
		{"unknown_notes", "weird_token", false, "Unknown token type"},
		{"fine_grained_silent", "github_pat_AAAA", false, ""},
		{"app_silent", "ghs_AAAA", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			advice, reject := github.New().AdviseToken(tc.token)
			if reject != tc.wantReject {
				t.Errorf("reject = %v, want %v", reject, tc.wantReject)
			}

			if tc.wantContains == "" && advice != "" {
				t.Errorf("advice = %q, want empty", advice)
			}

			if tc.wantContains != "" && !strings.Contains(advice, tc.wantContains) {
				t.Errorf("advice = %q, want contains %q", advice, tc.wantContains)
			}
		})
	}
}
