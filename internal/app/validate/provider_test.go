// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/internal/app/validate"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeprovider"
)

// --- Token --------------------------------------------------------------

func TestToken_GitHub_FineGrainedAPISucceeds(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	var buf bytes.Buffer
	err := appvalidate.Token(context.Background(), prov, &buf, appvalidate.TokenInput{
		Token: "github_pat_AAAA", Repository: "owner/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "GitHub token validated") {
		t.Errorf("output = %q", buf.String())
	}
	calls := prov.ValidateTokenCalls()
	if len(calls) != 1 || calls[0].Token != "github_pat_AAAA" || calls[0].Repo != "owner/repo" {
		t.Errorf("ValidateTokenCalls = %v", calls)
	}
}

func TestToken_GitHub_ClassicPATRefused(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token: "ghp_classic", Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "Classic PAT detected") {
		t.Errorf("err = %v", err)
	}
	if c := prov.Calls().ValidateToken; c != 0 {
		t.Errorf("ValidateToken should not be called for ghp_, got %d calls", c)
	}
}

func TestToken_GitHub_UnknownPrefixWarnsButProceeds(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	var buf bytes.Buffer
	if err := appvalidate.Token(context.Background(), prov, &buf, appvalidate.TokenInput{
		Token: "weird_token", Repository: "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Unknown token type") {
		t.Errorf("output = %q (should warn on unknown prefix)", buf.String())
	}
}

func TestToken_GitHub_AppToken(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	var buf bytes.Buffer
	if err := appvalidate.Token(context.Background(), prov, &buf, appvalidate.TokenInput{
		Token: "ghs_AAAA", Repository: "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}
	// ghs_ is recognized — no "unknown token" warning.
	if strings.Contains(buf.String(), "Unknown token type") {
		t.Errorf("ghs_ should not trigger unknown warning: %q", buf.String())
	}
}

func TestToken_EmptyToken(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "No GitHub token provided") {
		t.Errorf("err = %v", err)
	}
	for _, want := range []string{"fine-grained PAT", "personal-access-tokens"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %s", want, err.Error())
		}
	}
}

func TestToken_EmptyRepositoryUsage(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub)
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token: "github_pat_AAAA",
	})
	if err == nil || !strings.Contains(err.Error(), "No repository provided") {
		t.Errorf("err = %v", err)
	}
}

func TestToken_GitLab_NoFormatChecks(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitLab)
	var buf bytes.Buffer
	// "ghp_classic" would fail on GitHub; on GitLab we don't gate by prefix.
	if err := appvalidate.Token(context.Background(), prov, &buf, appvalidate.TokenInput{
		Token: "ghp_classic", Repository: "group/project",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "GitLab token validated") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestToken_APIRejection(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.PlatformGitHub).
		WithValidateTokenError(fakeError("HTTP 401"))
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token: "github_pat_AAAA", Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "Token is invalid") {
		t.Errorf("err = %v", err)
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }

// --- BotPermissions -----------------------------------------------------

func TestBotPermissions_AllProbesPass(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithBotPermissions(provider.BotPermissions{
		UserAccessible: true, RepoAccessible: true, BranchesAccessible: true,
	})
	var buf bytes.Buffer
	if err := appvalidate.BotPermissions(context.Background(), prov, &buf, appvalidate.BotPermissionsInput{
		Repository: "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Bot token is valid") {
		t.Errorf("output = %q", buf.String())
	}
	if strings.Contains(buf.String(), "may have limited permissions") {
		t.Errorf("warning leaked when all probes passed: %s", buf.String())
	}
}

func TestBotPermissions_BranchesWarnOnly(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithBotPermissions(provider.BotPermissions{
		UserAccessible: true, RepoAccessible: true, BranchesAccessible: false,
	})
	var buf bytes.Buffer
	if err := appvalidate.BotPermissions(context.Background(), prov, &buf, appvalidate.BotPermissionsInput{
		Repository: "owner/repo",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "may have limited permissions") {
		t.Errorf("expected warn block when BranchesAccessible=false, got: %s", buf.String())
	}
	for _, want := range []string{"Push commits", "Create and move tags", "Bypass branch protection"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %s", want, buf.String())
		}
	}
}

func TestBotPermissions_UserFatal(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithBotPermissions(provider.BotPermissions{
		UserAccessible: false, RepoAccessible: true, BranchesAccessible: true,
	})
	err := appvalidate.BotPermissions(context.Background(), prov, &bytes.Buffer{}, appvalidate.BotPermissionsInput{
		Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "RELEASE_TOKEN is invalid or expired") {
		t.Errorf("err = %v", err)
	}
}

func TestBotPermissions_RepoFatal(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithBotPermissions(provider.BotPermissions{
		UserAccessible: true, RepoAccessible: false, BranchesAccessible: true,
	})
	err := appvalidate.BotPermissions(context.Background(), prov, &bytes.Buffer{}, appvalidate.BotPermissionsInput{
		Repository: "owner/repo",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot access this repository") {
		t.Errorf("err = %v", err)
	}
}

func TestBotPermissions_EmptyRepoUsage(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)
	err := appvalidate.BotPermissions(context.Background(), prov, &bytes.Buffer{}, appvalidate.BotPermissionsInput{})
	if err == nil || !strings.Contains(err.Error(), "Usage") {
		t.Errorf("err = %v", err)
	}
}

// --- Authorization ------------------------------------------------------

func TestAuthorization_SnapshotBypass(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := appvalidate.Authorization(&buf, appvalidate.AuthorizationInput{
		Tag: "v1.0.0-SNAPSHOT", Actor: "eve", AuthorizedDevs: "alice,bob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "SNAPSHOT release") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestAuthorization_UppercaseSnapshotBypass(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := appvalidate.Authorization(&buf, appvalidate.AuthorizationInput{
		Tag: "v2.0.0-SNAPSHOT", Actor: "random-user", AuthorizedDevs: "admin1,admin2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "authorization check skipped") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestAuthorization_NoRestrictions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := appvalidate.Authorization(&buf, appvalidate.AuthorizationInput{
		Tag: "v1.0.0", Actor: "anyone",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"no restrictions configured", "All users with tag push access can create releases"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in %q", want, buf.String())
		}
	}
	if !strings.Contains(buf.String(), "no restrictions configured") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestAuthorization_Allowed(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := appvalidate.Authorization(&buf, appvalidate.AuthorizationInput{
		Tag: "v1.0.0", Actor: "bob", AuthorizedDevs: "alice,bob",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "User 'bob' is authorized") {
		t.Errorf("output = %q", buf.String())
	}
}

func TestAuthorization_TrailingCommaAllowed(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := appvalidate.Authorization(&buf, appvalidate.AuthorizationInput{
		Tag: "v1.0.0", Actor: "admin", AuthorizedDevs: "admin,developer,",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorization_NoPartialMatch(t *testing.T) {
	t.Parallel()
	err := appvalidate.Authorization(&bytes.Buffer{}, appvalidate.AuthorizationInput{
		Tag: "v1.0.0", Actor: "admin", AuthorizedDevs: "administrator,superadmin",
	})
	if err == nil || !strings.Contains(err.Error(), "User 'admin' is not authorized") {
		t.Errorf("err = %v", err)
	}
}

func TestAuthorization_Denied(t *testing.T) {
	t.Parallel()
	err := appvalidate.Authorization(&bytes.Buffer{}, appvalidate.AuthorizationInput{
		Tag: "v1.0.0", Actor: "eve", AuthorizedDevs: "alice,bob",
	})
	if err == nil {
		t.Fatal("expected denial error")
	}
	for _, want := range []string{
		"User 'eve' is not authorized",
		"  - alice",
		"  - bob",
		"SNAPSHOT release instead",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err missing %q: %s", want, err.Error())
		}
	}
}

func TestAuthorization_EmptyArgsUsage(t *testing.T) {
	t.Parallel()
	err := appvalidate.Authorization(&bytes.Buffer{}, appvalidate.AuthorizationInput{Tag: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "Usage") {
		t.Errorf("err = %v", err)
	}
}
