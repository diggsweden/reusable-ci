// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// noEnv is a fixed, empty environment. github.New() reads os.Getenv, and a
// Forgejo runner sets GITHUB_SERVER_URL for compatibility -- measured: with
// GITHUB_SERVER_URL=https://codeberg.org the two tests below failed, one on the
// issuer and one on PublicFulcioTrusted. The suite's expectations about
// github.com must not depend on which forge happens to run it.
func noEnv(string) string { return "" }

func TestDescribe_GitHub(t *testing.T) {
	t.Parallel()

	info := (&github.Provider{Env: noEnv}).Describe()
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

// TestCapabilities_GitHub compares the whole struct from a fixed environment.
// The previous check read five fields and said "want all true", which was
// neither what it checked nor true: ContainerTagDeletion and
// ContainerPackageListing are false for this adapter, and a capability added
// later would default to false with nothing noticing.
func TestCapabilities_GitHub(t *testing.T) {
	t.Parallel()

	want := provider.Capabilities{
		SARIFUpload:         true,
		Attestation:         true,
		ReleaseAssets:       true,
		RunArtifacts:        true,
		MintsOIDCToken:      true,
		PublicFulcioTrusted: true,
		// GitHub's package API is not wired to the tag-deletion and listing
		// roles, so base-image staging cleanup is not offered here.
		ContainerTagDeletion:    false,
		ContainerPackageListing: false,
	}
	if got := (&github.Provider{Env: noEnv}).Capabilities(); got != want {
		t.Errorf("Capabilities = %+v\nwant                %+v", got, want)
	}
}

func TestDescribe_GHESUsesInstanceIssuer(t *testing.T) {
	t.Parallel()

	p := github.New()
	p.Env = func(key string) string {
		if key == "GITHUB_SERVER_URL" {
			return "https://github.acme.example"
		}

		return ""
	}

	if got, want := p.Describe().OIDCIssuer, "https://github.acme.example/_services/token"; got != want {
		t.Fatalf("OIDCIssuer = %q, want %q", got, want)
	}

	if p.Capabilities().PublicFulcioTrusted {
		t.Fatal("GHES issuer must not be reported as trusted by public Fulcio")
	}

	if !p.Capabilities().MintsOIDCToken {
		t.Fatal("GHES still mints OIDC tokens for an explicitly configured Fulcio")
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
