// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package fakeprovider_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// Compile-time check: Fake satisfies provider.Provider.
var _ provider.Provider = (*fakeprovider.Fake)(nil)

func TestFake_Name(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform provider.ForgeAPI
		want     provider.ForgeAPI
	}{
		{name: "defaults_to_local", want: provider.ForgeLocal},
		{name: "configured_platform", platform: provider.ForgeGitHub, want: provider.ForgeGitHub},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

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
	t.Parallel()

	for _, platform := range []provider.ForgeAPI{"", provider.ForgeGitLab} {
		t.Run(string(platform), func(t *testing.T) {
			t.Parallel()

			want := provider.EventContext{
				ForgeAPI: platform,
				RefName:  "feat/x", RefType: provider.RefTypePR,
				SHA: "abcdef0123456789abcdef0123456789abcdef0123", ShortSHA: "abcdef0",
				Branch: "feat/x", PRNumber: "42", EventName: "pull_request",
				Repo: "owner/repo", RepoURL: "https://forge.example.invalid/owner/repo",
			}
			configured := want
			fake := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithEventContext(configured)
			configured.Repo = "caller mutation"

			if want.ForgeAPI == "" {
				want.ForgeAPI = provider.ForgeGitHub
			}

			got, err := fake.ResolveContext(t.Context())
			require.NoError(t, err)
			require.Equal(t, &want, got)
			*got = provider.EventContext{}
			again, err := fake.ResolveContext(t.Context())
			require.NoError(t, err)
			require.Equal(t, &want, again)
			require.Equal(t, fakeprovider.Calls{ResolveContext: 2}, fake.Calls())
		})
	}
}

func TestFake_ResolveContextDefaults(t *testing.T) {
	t.Parallel()

	for _, platform := range provider.AllForges() {
		fake := fakeprovider.New(t).WithPlatform(platform)
		got, err := fake.ResolveContext(t.Context())
		require.NoError(t, err)
		require.Equal(t, &provider.EventContext{ForgeAPI: platform}, got)
		require.Equal(t, fakeprovider.Calls{ResolveContext: 1}, fake.Calls())
	}
}

func TestFake_ReleaseRecordingOwnership(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"create", "publish"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			wantErr := errors.New("release rejected") //nolint:err113 // test mock error
			fake := fakeprovider.New(t).WithCreateReleaseError(wantErr).WithPublishReleaseError(wantErr)
			invoke, snapshot := fake.CreateRelease, fake.CreateReleaseCalls
			wantCount := fakeprovider.Calls{CreateRelease: 4}

			if method == "publish" {
				invoke, snapshot = fake.PublishRelease, fake.PublishReleaseCalls
				wantCount = fakeprovider.Calls{PublishRelease: 4}
			}

			require.Empty(t, snapshot())

			want := []fakeprovider.ReleaseCall{
				{Repo: "owner/first", Spec: provider.ReleaseSpec{
					Tag: "v1", Name: "First", NotesFile: "notes.md", Draft: true,
					Prerelease: true, MakeLatest: provider.MakeLatestFalse, Assets: []string{"one.zip", "two.zip"},
				}},
				{Repo: "owner/second", Spec: provider.ReleaseSpec{Tag: "v2", Assets: []string{"three.zip"}}},
				{Repo: "owner/nil", Spec: provider.ReleaseSpec{Tag: "v3"}},
				{Repo: "owner/empty", Spec: provider.ReleaseSpec{Tag: "v4", Assets: []string{}}},
			}
			for _, call := range want {
				spec := call.Spec
				spec.Assets = slices.Clone(spec.Assets)
				require.ErrorIs(t, invoke(t.Context(), call.Repo, spec), wantErr)

				for index := range spec.Assets {
					spec.Assets[index] = "caller mutation"
				}
			}

			got := snapshot()
			require.Equal(t, want, got)
			require.Equal(t, wantCount, fake.Calls())

			for index := range got {
				got[index].Repo = "snapshot mutation"

				got[index].Spec.Tag = "wrong tag"
				for asset := range got[index].Spec.Assets {
					got[index].Spec.Assets[asset] = "snapshot mutation"
				}
			}

			require.Equal(t, want, snapshot())
		})
	}
}

func TestFake_SARIFRecordingOwnership(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("upload rejected") //nolint:err113 // test mock error
	fake := fakeprovider.New(t).WithUploadSARIFError(wantErr)
	require.Empty(t, fake.UploadSARIFCalls())

	want := []provider.SARIFUpload{
		{Repository: "owner/first", SHA: "abc123", Ref: "refs/heads/main", Token: "synthetic-first", SARIF: []byte(`{"runs":[]}`)},
		{Repository: "owner/second", SHA: "def456", Ref: "refs/heads/dev", Token: "synthetic-second", SARIF: []byte("second\n")},
		{Repository: "owner/nil"},
		{Repository: "owner/empty", SARIF: []byte{}},
	}
	for _, upload := range want {
		upload.SARIF = slices.Clone(upload.SARIF)
		require.ErrorIs(t, fake.UploadSARIF(t.Context(), upload), wantErr)

		for index := range upload.SARIF {
			upload.SARIF[index] = 'X'
		}
	}

	got := fake.UploadSARIFCalls()
	require.Equal(t, want, got)

	for index := range got {
		got[index].Repository = "snapshot mutation"
		for pos := range got[index].SARIF {
			got[index].SARIF[pos] = 'Y'
		}
	}

	require.Equal(t, want, fake.UploadSARIFCalls())
}

func TestFake_CapabilityDefaults(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		platform provider.ForgeAPI
		issuer   string
		want     provider.Capabilities
	}{
		{platform: provider.ForgeGitHub, issuer: "https://token.actions.githubusercontent.com", want: provider.Capabilities{
			SARIFUpload: true, Attestation: true, ReleaseAssets: true, MintsOIDCToken: true, PublicFulcioTrusted: true,
		}},
		{platform: provider.ForgeGitLab, issuer: "https://gitlab.com", want: provider.Capabilities{
			ReleaseAssets: true, MintsOIDCToken: true, PublicFulcioTrusted: true,
		}},
		{platform: provider.ForgeForgejo, want: provider.Capabilities{
			ReleaseAssets: true,
		}},
		{platform: provider.ForgeLocal},
		{platform: provider.ForgeAPI("unknown")},
	} {
		t.Run(string(test.platform), func(t *testing.T) {
			t.Parallel()

			fake := fakeprovider.New(t).WithPlatform(test.platform)
			got := fake.Capabilities()
			require.Equal(t, test.want, got)
			require.Equal(t, test.issuer, fake.Describe().OIDCIssuer)
			require.Equal(t, provider.PublicFulcioTrusts(test.issuer), got.PublicFulcioTrusted)

			if got.PublicFulcioTrusted {
				require.True(t, got.MintsOIDCToken)
			}
		})
	}
}

func TestFake_ResolveContextError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom") //nolint:err113 // test mock error
	f := fakeprovider.New(t).WithResolveContextError(wantErr)

	_, err := f.ResolveContext(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestFake_RecordsCalls(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	releaseErr := errors.New("release failed") //nolint:err113 // test mock error
	uploadErr := errors.New("upload failed")   //nolint:err113 // test mock error
	tokenErr := errors.New("token failed")     //nolint:err113 // test mock error
	botErr := errors.New("bot failed")         //nolint:err113 // test mock error
	repoErr := errors.New("repo failed")       //nolint:err113 // test mock error

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

	if err := f.UploadSARIF(context.Background(), provider.SARIFUpload{Repository: "owner/repo"}); !errors.Is(err, uploadErr) {
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

	if got := f.UploadSARIFCalls(); len(got) != 1 || got[0].Repository != "owner/repo" {
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

// Two properties of this fake are worth pinning, and they are the two that
// constrain its implementation rather than its configuration.
//
// Most of what a provider fake could be asked to prove is vacuous by
// construction: asserting that WithRepoMetadata makes FetchRepoMetadata return
// that metadata, or that the description default is the description default,
// restates the fixture the test just set. What cannot be read off the
// configuration is whether the recorder hands out its own storage, and whether
// one call counts as one call.

// TestFake_RecordedArgumentsAreDetachedSnapshots proves an assertion cannot
// corrupt what a later assertion reads.
//
// Every recorder accessor builds a copy, but nothing checked it. Returning the
// backing slice would make the first test that sorts or truncates a recorded
// call list silently change what every later read of the same fake sees — and
// the symptom would appear in an unrelated assertion, which is the hardest kind
// of test failure to trace.
func TestFake_RecordedArgumentsAreDetachedSnapshots(t *testing.T) {
	t.Parallel()

	fake := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	for _, repo := range []string{"owner/one", "owner/two"} {
		//nolint:gosec // G101: fake token literal, not a credential.
		if err := fake.ValidateToken(t.Context(), "github_pat_fixture", repo); err != nil {
			t.Fatal(err)
		}

		if _, err := fake.ValidateBotPermissions(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
	}

	tokenCalls := fake.ValidateTokenCalls()
	botArgs := fake.ValidateBotPermissionsArgs()

	if len(tokenCalls) != 2 || len(botArgs) != 2 {
		t.Fatalf("recorded %d token and %d bot calls, want 2 each", len(tokenCalls), len(botArgs))
	}

	// Corrupt what we were handed.
	tokenCalls[0].Repo = "mutated"
	tokenCalls[1] = fakeprovider.TokenCall{}
	botArgs[0] = "mutated"

	if got := fake.ValidateTokenCalls(); got[0].Repo != "owner/one" || got[1].Repo != "owner/two" {
		t.Errorf("token calls after caller mutation = %+v, want the recorded pair", got)
	}

	if got := fake.ValidateBotPermissionsArgs(); !slices.Equal(got, []string{"owner/one", "owner/two"}) {
		t.Errorf("bot args after caller mutation = %v, want the recorded pair", got)
	}
}

// TestFake_CountersCountEachCallExactlyOnce pins the counters against the two
// ways they go wrong: not incrementing, and incrementing more than once.
//
// Tests assert "this was called once" or "this was never called" through these,
// so a counter that double-counts turns a correct product into a failing test,
// and one that never counts makes "never called" true no matter what happened —
// which is the direction that hides a bug rather than inventing one.
func TestFake_CountersCountEachCallExactlyOnce(t *testing.T) {
	t.Parallel()

	fake := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	if got := fake.Calls(); got != (fakeprovider.Calls{}) {
		t.Fatalf("a fresh fake reports %+v, want all zero", got)
	}

	_ = fake.Name()

	if _, err := fake.FetchRepoMetadata(t.Context(), "owner/repo"); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // G101: fake token literal, not a credential.
	if err := fake.ValidateToken(t.Context(), "github_pat_fixture", "owner/repo"); err != nil {
		t.Fatal(err)
	}

	got := fake.Calls()
	if got.Name != 1 || got.FetchRepoMetadata != 1 || got.ValidateToken != 1 {
		t.Errorf("counters = %+v, want exactly one of each called method", got)
	}

	// Methods that were not called stay at zero, so "never called" means it.
	if got.CreateRelease != 0 || got.PublishRelease != 0 || got.UploadReleaseAsset != 0 || got.ValidateBotPermissions != 0 {
		t.Errorf("counters = %+v, want zero for the methods never invoked", got)
	}
}
