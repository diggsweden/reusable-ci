// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"testing"

	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

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

// TestKeylessVerifyIdentity_LocalIsNoop covers the forges with no keyless
// support of their own -- a local checkout, and Forgejo out of the box. Both
// take the same branch: no resolver, or one that reports SupportsKeyless()
// false, so whatever the operator passed comes back untouched.
//
// Returning empty constraints looks alarming, because they travel unchanged
// into `cosign verify --certificate-identity-regexp ""`, and an empty regexp
// matches every identity. It is safe, and the reason is entirely outside this
// package: cosign refuses the flag rather than honouring it --
//
//	Error: --certificate-identity or --certificate-identity-regexp is
//	required for verification in keyless mode
//
// (verified against cosign 3.1.3). So the operator on Forgejo must supply the
// constraints or verification fails; it never silently accepts any signer.
// Recorded here because the question is natural, the answer is three packages
// away, and it rests on cosign's behaviour rather than on ours.
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

// TestVerificationIdentity_ForgeOverrideAndKMSMatrix covers every cell the
// two signature validators share: each forge the runner can be detected as,
// with neither, one or both constraints pinned, a GitHub runner whose identity
// cannot be resolved, and the KMS cases, where nothing is injected whatever
// the forge. Pinned values always survive, and a derived value only fills an
// empty one.
//
// Not parallel: forge detection reads the process environment.
func TestVerificationIdentity_ForgeOverrideAndKMSMatrix(t *testing.T) {
	const (
		pinnedRegexp = `^https://github\.com/other/`
		pinnedIssuer = "https://issuer.example"
	)

	forges := map[string]map[string]string{
		"github":                    {"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "acme/app", "GITHUB_SERVER_URL": "https://github.com"},
		"gitlab":                    {"GITLAB_CI": "true", "CI_SERVER_URL": "https://gitlab.example", "CI_PROJECT_URL": "https://gitlab.example/group/app"},
		"github without repository": {"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com"},
		"local":                     {},
	}

	for forge, env := range forges {
		for _, pin := range [][2]string{{"", ""}, {pinnedRegexp, ""}, {"", pinnedIssuer}, {pinnedRegexp, pinnedIssuer}} {
			for _, kms := range []kmsSelector{{"", ""}, {domainrelease.SignMethodSigstore, ""}, {"", "awskms:///alias/release"}, {domainrelease.SignMethodKMS, ""}} {
				t.Run(forge+"/"+pin[0]+"|"+pin[1]+"/"+string(kms.method)+"|"+kms.key, func(t *testing.T) {
					for _, key := range []string{"GITHUB_ACTIONS", "GITHUB_REPOSITORY", "GITHUB_SERVER_URL", "GITLAB_CI", "CI_SERVER_URL", "CI_PROJECT_URL"} {
						t.Setenv(key, env[key])
					}

					clearForgejoSignals(t)

					want := expectedVerificationIdentity(forge, pin, kms)

					gotRegexp, gotIssuer := verificationIdentity(kms.method, kms.key, pin[0], pin[1])
					if gotRegexp != want[0] || gotIssuer != want[1] {
						t.Errorf("got (%q, %q), want (%q, %q)", gotRegexp, gotIssuer, want[0], want[1])
					}
				})
			}
		}
	}
}

// kmsSelector is the method and key that decide whether verification is KMS.
type kmsSelector struct {
	method domainrelease.SignMethod
	key    string
}

// expectedVerificationIdentity keeps pinned values and, outside KMS
// verification on a forge with a resolvable identity, fills the empty ones
// from that forge.
func expectedVerificationIdentity(forge string, pin [2]string, kms kmsSelector) [2]string {
	derived := map[string][2]string{
		"github": {`^https://github\.com/acme/app/`, "https://token.actions.githubusercontent.com"},
		"gitlab": {`^https://gitlab\.example/group/app/`, "https://gitlab.example"},
	}

	values, ok := derived[forge]
	if !ok || kms.key != "" || kms.method == domainrelease.SignMethodKMS {
		return pin
	}

	want := pin

	for i := range want {
		if want[i] == "" {
			want[i] = values[i]
		}
	}

	return want
}
