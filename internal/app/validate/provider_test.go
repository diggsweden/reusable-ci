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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// --- Token --------------------------------------------------------------

func TestToken_GitHub_FineGrainedAPISucceeds(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	var buf bytes.Buffer

	//nolint:gosec // fake token literal — not a real credential.
	err := appvalidate.Token(context.Background(), prov, &buf, appvalidate.TokenInput{
		Token:      "github_pat_AAAA", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Repository: "owner/repo",      //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token: "ghp_classic", Repository: "owner/repo",
	})
	// A token the policy will not accept is a credential refusal (exit 77).
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if !strings.Contains(err.Error(), "classic PAT detected") {
		t.Errorf("err = %v, want it to name the token type", err)
	}

	if c := prov.Calls().ValidateToken; c != 0 {
		t.Errorf("ValidateToken should not be called for ghp_, got %d calls", c)
	}
}

func TestToken_GitHub_UnknownPrefixWarnsButProceeds(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

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
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

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
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Repository: "owner/repo",
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if !strings.Contains(err.Error(), "no GitHub token provided") {
		t.Errorf("err = %v, want it to say the token is absent", err)
	}

	for _, want := range []string{"fine-grained PAT", "personal-access-tokens"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %s", want, err.Error())
		}
	}
}

func TestToken_EmptyRepositoryUsage(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	//nolint:gosec // fake token literal — not a real credential.
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token: "github_pat_AAAA",
	})
	// A missing --repository is a broken command line, not a credential
	// problem: ErrUsage (exit 2), unlike the refusals above.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "no repository provided") {
		t.Errorf("err = %v, want it to name the missing argument", err)
	}
}

func TestToken_GitLab_NoFormatChecks(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitLab)

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
	prov := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).
		WithValidateTokenError(fmt.Errorf("HTTP 401: %w", errs.ErrPermissionDenied))

	//nolint:gosec // fake token literal — not a real credential.
	err := appvalidate.Token(context.Background(), prov, &bytes.Buffer{}, appvalidate.TokenInput{
		Token:      "github_pat_AAAA",
		Repository: "owner/repo",
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	// The provider's own message survives, so an operator sees the HTTP status.
	for _, want := range []string{"validate token", "HTTP 401"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

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
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if !strings.Contains(err.Error(), "RELEASE_TOKEN is invalid or expired") {
		t.Errorf("err = %v, want it to name the token", err)
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
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}

	if !strings.Contains(err.Error(), "cannot access this repository") {
		t.Errorf("err = %v, want it to name the access problem", err)
	}
}

func TestBotPermissions_EmptyRepoUsage(t *testing.T) {
	t.Parallel()
	prov := fakeprovider.New(t)

	err := appvalidate.BotPermissions(context.Background(), prov, &bytes.Buffer{}, appvalidate.BotPermissionsInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Nothing probed: an empty repository must not reach the forge API.
	if c := prov.Calls().ValidateBotPermissions; c != 0 {
		t.Errorf("probed the forge with no repository, %d calls", c)
	}
}
