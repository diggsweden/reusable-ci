// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import "testing"

// clearForgejoSignals neutralises the Forgejo probe so a GITHUB_ACTIONS=true
// environment detects as GitHub, not Forgejo.
func clearForgejoSignals(t *testing.T) {
	t.Helper()
	t.Setenv("FORGEJO_ACTIONS", "")
	t.Setenv("FORGEJO_SERVER_URL", "")
	t.Setenv("FORGEJO_REPOSITORY", "")
	t.Setenv("FORGEJO_OUTPUT", "")
}

func TestKeylessVerifyIdentity_DefaultsFromGitHub(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITLAB_CI", "")
	t.Setenv("GITHUB_REPOSITORY", "acme/app")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	clearForgejoSignals(t)

	gotRe, gotIss := keylessVerifyIdentity("", "")

	if want := `^https://github\.com/acme/app/`; gotRe != want {
		t.Errorf("identity regexp = %q, want %q", gotRe, want)
	}

	if want := "https://token.actions.githubusercontent.com"; gotIss != want {
		t.Errorf("oidc issuer = %q, want %q", gotIss, want)
	}
}

func TestKeylessVerifyIdentity_ExplicitWins(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_REPOSITORY", "acme/app")
	clearForgejoSignals(t)

	gotRe, gotIss := keylessVerifyIdentity(`^https://github\.com/other/`, "https://issuer.example")

	if gotRe != `^https://github\.com/other/` || gotIss != "https://issuer.example" {
		t.Errorf("explicit values should win, got regexp=%q issuer=%q", gotRe, gotIss)
	}
}

func TestKeylessVerifyIdentity_LocalIsNoop(t *testing.T) {
	// No CI signal → local forge → no SigningIdentityResolver → no-op.
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITLAB_CI", "")
	clearForgejoSignals(t)

	gotRe, gotIss := keylessVerifyIdentity("", "")

	if gotRe != "" || gotIss != "" {
		t.Errorf("local forge should leave constraints empty, got regexp=%q issuer=%q", gotRe, gotIss)
	}
}
