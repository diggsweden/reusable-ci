// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package fakeprovider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeprovider"
)

// Compile-time check: Fake satisfies provider.Provider.
var _ provider.Provider = (*fakeprovider.Fake)(nil)

func TestFake_Name(t *testing.T) {
	tests := []struct {
		name     string
		platform provider.Platform
		want     provider.Platform
	}{
		{name: "defaults_to_local", want: provider.PlatformLocal},
		{name: "configured_platform", platform: provider.PlatformGitHub, want: provider.PlatformGitHub},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			f := fakeprovider.New(t)
			if testCase.platform != "" {
				f.WithPlatform(testCase.platform)
			}

			if got := f.Name(); got != testCase.want {
				t.Errorf("Name() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFake_ResolveContextReturnsConfigured(t *testing.T) {
	want := provider.EventContext{
		RefName: "v1.2.3",
		RefType: provider.RefTypeTag,
		SHA:     "abcdef0123456789",
		Repo:    "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	f := fakeprovider.New(t).
		WithPlatform(provider.PlatformGitHub).
		WithEventContext(want)

	got, err := f.ResolveContext(context.Background())
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	if got.RefName != want.RefName || got.RefType != want.RefType || got.SHA != want.SHA {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if got.Platform != provider.PlatformGitHub {
		t.Errorf("Platform not propagated, got %q", got.Platform)
	}
}

func TestFake_ResolveContextError(t *testing.T) {
	wantErr := errors.New("boom") //nolint:err113 // test mock error
	f := fakeprovider.New(t).WithResolveContextError(wantErr)

	_, err := f.ResolveContext(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestFake_RecordsCalls(t *testing.T) {
	f := fakeprovider.New(t)
	_ = f.Name()
	_ = f.Name()
	_, _ = f.ResolveContext(context.Background())

	c := f.Calls() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if c.Name != 2 {
		t.Errorf("Name calls = %d, want 2", c.Name)
	}

	if c.ResolveContext != 1 {
		t.Errorf("ResolveContext calls = %d, want 1", c.ResolveContext)
	}
}

//nolint:cyclop // covers every method on the fake provider.
func TestFake_ProviderMethodResponsesAndRecorders(t *testing.T) {
	releaseErr := errors.New("release failed") //nolint:err113 // test mock error
	uploadErr := errors.New("upload failed") //nolint:err113 // test mock error
	tokenErr := errors.New("token failed") //nolint:err113 // test mock error
	botErr := errors.New("bot failed") //nolint:err113 // test mock error
	repoErr := errors.New("repo failed") //nolint:err113 // test mock error

	f := fakeprovider.New(t). //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		WithRepoMetadata(provider.RepoMetadata{Description: "repo", LicenseSPDX: "Apache-2.0"}).
		WithBotPermissions(provider.BotPermissions{UserAccessible: true}).
		WithCreateReleaseError(releaseErr).
		WithUploadSARIFError(uploadErr)

	meta, err := f.FetchRepoMetadata(context.Background(), "owner/repo")
	if err != nil || meta.Description != "repo" || meta.LicenseSPDX != "Apache-2.0" {
		t.Fatalf("FetchRepoMetadata = %+v, %v", meta, err)
	}

	perms, err := f.ValidateBotPermissions(context.Background(), "owner/repo")
	if err != nil || !perms.UserAccessible {
		t.Fatalf("ValidateBotPermissions = %+v, %v", perms, err)
	}

	if err := f.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{Tag: "v1.0.0"}); !errors.Is(err, releaseErr) {
		t.Fatalf("CreateRelease err = %v", err)
	}

	if err := f.UploadSARIF(context.Background(), provider.SARIFUpload{Repository: "owner/repo", Category: "scan"}); !errors.Is(err, uploadErr) {
		t.Fatalf("UploadSARIF err = %v", err)
	}

	if got := f.FetchRepoArgs(); len(got) != 1 || got[0] != "owner/repo" {
		t.Errorf("FetchRepoArgs = %v", got)
	}

	if got := f.ValidateBotPermissionsArgs(); len(got) != 1 || got[0] != "owner/repo" {
		t.Errorf("ValidateBotPermissionsArgs = %v", got)
	}

	if got := f.CreateReleaseCalls(); len(got) != 1 || got[0].Repo != "owner/repo" || got[0].Spec.Tag != "v1.0.0" {
		t.Errorf("CreateReleaseCalls = %+v", got)
	}

	if got := f.UploadSARIFCalls(); len(got) != 1 || got[0].Repository != "owner/repo" || got[0].Category != "scan" {
		t.Errorf("UploadSARIFCalls = %+v", got)
	}

	f = fakeprovider.New(t).
		WithFetchRepoMetadataError(repoErr).
		WithValidateTokenError(tokenErr).
		WithValidateBotPermissionsError(botErr)
	if _, err := f.FetchRepoMetadata(context.Background(), "owner/repo"); !errors.Is(err, repoErr) {
		t.Errorf("FetchRepoMetadata err = %v", err)
	}

	if err := f.ValidateToken(context.Background(), "tok", "owner/repo"); !errors.Is(err, tokenErr) {
		t.Errorf("ValidateToken err = %v", err)
	}

	if _, err := f.ValidateBotPermissions(context.Background(), "owner/repo"); !errors.Is(err, botErr) {
		t.Errorf("ValidateBotPermissions err = %v", err)
	}

	if got := f.ValidateTokenCalls(); len(got) != 1 || got[0].Token != "tok" || got[0].Repo != "owner/repo" {
		t.Errorf("ValidateTokenCalls = %+v", got)
	}
}
