// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// The credential validators in this package all take a secret and then talk
// about it: they print which secret was missing, warn about a token's shape,
// and wrap a provider's rejection. Any of those lines can carry the value
// itself, and everything they write goes to a CI log.
//
// The existing tests could not see that happen. The Maven fixtures are "u" and
// "p" — one character each, and both are substrings of ordinary words in the
// surrounding prose, so a test asserting their absence would fail on the word
// "publishing". The token fixtures are short and prefix-shaped, and their
// prefixes are echoed on purpose, so "does the output contain the token" has no
// clean answer either. Printing the Maven username and password verbatim left
// all three Maven tests passing; printing the token before validating it left
// all eight token tests passing.
//
// A canary fixes both problems at once: long enough to be unambiguous, and
// shaped so nothing else could produce it. The assertions below then read the
// same way for every surface — stdout, stderr, annotations and the returned
// error — because a leak is a leak wherever it lands.
const (
	canaryUser     = "CANARY-USERNAME-4c1f9a2be7d84f0e93c6"
	canaryPassword = "CANARY-PASSWORD-7f3e5d1ac9b24608e5a7"
	canarySecret   = "CANARY-TOKENBODY-2b8d6e4f0a1c7395df82"
)

// assertNoCanary fails when any watched surface carries a canary value.
func assertNoCanary(t *testing.T, surfaces map[string]string, canaries ...string) {
	t.Helper()

	for name, body := range surfaces {
		for _, canary := range canaries {
			if strings.Contains(body, canary) {
				t.Errorf("%s leaked a credential (%s):\n%s", name, canary, body)
			}
		}
	}
}

func TestMavenCentralCredentials_NeverEchoesTheSecrets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		in      appvalidate.MavenCentralCredentialsInput
		wantErr bool
	}{
		{
			name: "both present",
			in:   appvalidate.MavenCentralCredentialsInput{Username: canaryUser, Password: canaryPassword},
		},
		{
			// The password is still supplied here: the refusal is about the
			// username, and a diagnostic that dumps the whole input would
			// take the password with it.
			name:    "username missing",
			in:      appvalidate.MavenCentralCredentialsInput{Password: canaryPassword},
			wantErr: true,
		},
		{
			name:    "password missing",
			in:      appvalidate.MavenCentralCredentialsInput{Username: canaryUser},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out, stderr bytes.Buffer

			err := appvalidate.MavenCentralCredentials(&out, &stderr,
				output.NewAnnotator(&stderr, output.FormatGitHub), tc.in)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			surfaces := map[string]string{"stdout": out.String(), "stderr+annotations": stderr.String()}
			if err != nil {
				surfaces["error"] = err.Error()
			}

			assertNoCanary(t, surfaces, canaryUser, canaryPassword)
		})
	}
}

func TestToken_NeverEchoesTheTokenBody(t *testing.T) {
	t.Parallel()

	// Each token keeps a real, recognisable prefix — the prefix is operator
	// configuration the diagnostics are supposed to name — followed by the
	// canary, which is the part that must never appear.
	for _, tc := range []struct {
		name     string
		token    string
		platform provider.ForgeAPI
		apiErr   error
		wantErr  bool
	}{
		{
			name:     "a valid fine-grained token",
			token:    "github_pat_" + canarySecret,
			platform: provider.ForgeGitHub,
		},
		{
			name:     "a refused classic PAT",
			token:    "ghp_" + canarySecret,
			platform: provider.ForgeGitHub,
			wantErr:  true,
		},
		{
			name:     "an unknown prefix that only warns",
			token:    "weird_" + canarySecret,
			platform: provider.ForgeGitHub,
		},
		{
			// The provider's own error is the most likely carrier: an API
			// client that echoes the request can put the credential inside
			// the cause, which then gets wrapped and printed.
			name:     "the provider rejects the token and names it",
			token:    "github_pat_" + canarySecret,
			platform: provider.ForgeGitHub,
			apiErr:   fmt.Errorf("401 unauthorized for token github_pat_%s: %w", canarySecret, errUpstreamRejected),
			wantErr:  true,
		},
		{
			name:     "GitLab, which applies no format policy",
			token:    "glpat-" + canarySecret,
			platform: provider.ForgeGitLab,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prov := fakeprovider.New(t).WithPlatform(tc.platform)
			if tc.apiErr != nil {
				prov = prov.WithValidateTokenError(tc.apiErr)
			}

			var out bytes.Buffer

			err := appvalidate.Token(context.Background(), prov, &out,
				appvalidate.TokenInput{Token: tc.token, Repository: "owner/repo"})
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			surfaces := map[string]string{"stdout": out.String()}
			if err != nil {
				surfaces["error"] = err.Error()
			}

			assertNoCanary(t, surfaces, canarySecret)
		})
	}
}

// errUpstreamRejected stands in for a provider error that quotes the request.
var errUpstreamRejected = errors.New("upstream rejected the credential") //nolint:err113 // test fixture sentinel.

// TestPrerequisites_NeverEchoesSecretsOnAnySurface runs the whole aggregate
// with canary secrets and then renders the operator-facing summary from its
// result.
//
// The per-validator tests above watch one function's two writers. This watches
// what actually reaches an operator: the aggregate writer, the annotator, each
// check's captured Output, each check's Err, and the Markdown summary built
// from them. The aggregate is where a leak is most likely to survive review,
// because no single validator has to be wrong for it to happen — the summary
// composes fragments from all of them, and a fragment that quotes its input
// travels into the panel intact.
//
// Both credential-carrying validators are made to fail here, since the refusal
// paths are the ones that build messages out of the input.
func TestPrerequisites_NeverEchoesSecretsOnAnySurface(t *testing.T) {
	t.Chdir(t.TempDir())

	var out, annotations bytes.Buffer

	prov := fakeprovider.New(t).
		WithPlatform(provider.ForgeGitHub).
		WithValidateTokenError(fmt.Errorf("401 for ghp_%s: %w", canarySecret, errUpstreamRejected))

	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc1234"},
		Provider: prov,
	}, &out, output.NewAnnotator(&annotations, output.FormatGitHub),
		appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic canaries passed only to in-process fakes.
			RefType:               "tag",
			Tag:                   "v1.0.0",
			Ref:                   "refs/tags/v1.0.0",
			Repository:            "owner/repo",
			ReleaseToken:          "ghp_" + canarySecret,
			HasMavenCentralTarget: true,
			MavenCentralUsername:  canaryUser,
			// Absent on purpose: the refusal names the missing password
			// while the username is present, which is the shape that most
			// invites a diagnostic to dump the whole input.
		})

	surfaces := map[string]string{
		"aggregate stdout": out.String(),
		"annotations":      annotations.String(),
		"summary":          summaryFor(t, result.Checks...),
	}

	for _, check := range result.Checks {
		surfaces["check "+check.Name+" output"] = check.Output

		if check.Err != nil {
			surfaces["check "+check.Name+" error"] = check.Err.Error()
		}
	}

	if len(result.Checks) == 0 {
		t.Fatal("no checks ran; the assertions below would be vacuous")
	}

	assertNoCanary(t, surfaces, canaryUser, canaryPassword, canarySecret)
}
