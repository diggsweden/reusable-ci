// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package local_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/local"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestProvider_Name(t *testing.T) {
	t.Parallel()
	if got := local.New().Name(); got != provider.PlatformLocal {
		t.Errorf("Name = %q", got)
	}
}

func TestResolveContext_FromInjectedEnv(t *testing.T) {
	t.Parallel()
	envFunc := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	p := &local.Provider{Env: envFunc(map[string]string{
		"CI_REF_NAME": "main",
		"CI_COMMIT":   "abcdef0123456789",
		"CI_BRANCH":   "main",
		"CI_REPO":     "owner/repo",
	})}
	evt, _ := p.ResolveContext(context.Background())
	if evt.Platform != provider.PlatformLocal {
		t.Errorf("Platform = %q", evt.Platform)
	}
	if evt.RefName != "main" || evt.Branch != "main" || evt.Repo != "owner/repo" {
		t.Errorf("evt = %+v", evt)
	}
	if evt.SHA != "abcdef0123456789" || evt.ShortSHA != "abcdef0" {
		t.Errorf("SHA=%q ShortSHA=%q", evt.SHA, evt.ShortSHA)
	}
}

func TestResolveContext_EmptyEnv(t *testing.T) {
	t.Parallel()
	p := &local.Provider{Env: func(string) string { return "" }}
	evt, err := p.ResolveContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if evt.Platform != provider.PlatformLocal {
		t.Errorf("Platform = %q", evt.Platform)
	}
	// Other fields all empty / zero — that's the contract.
}

func TestCreateRelease_IsUnsupported(t *testing.T) {
	t.Parallel()
	err := local.New().CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{Tag: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "release creation requires GitHub Actions or GitLab CI") {
		t.Errorf("err = %v", err)
	}
}

func TestFetchRepoMetadata_ReturnsEmptyMetadata(t *testing.T) {
	t.Parallel()
	metadata, err := local.New().FetchRepoMetadata(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if metadata == nil {
		t.Fatal("FetchRepoMetadata returned nil")
	}
	if *metadata != (provider.RepoMetadata{}) {
		t.Errorf("metadata = %+v, want empty", metadata)
	}
}

func TestValidateToken_IsUnsupported(t *testing.T) {
	t.Parallel()
	err := local.New().ValidateToken(context.Background(), "owner/repo", "token")
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestValidateBotPermissions_ReturnsEmptyPermissions(t *testing.T) {
	t.Parallel()
	permissions, err := local.New().ValidateBotPermissions(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if permissions == nil {
		t.Fatal("ValidateBotPermissions returned nil")
	}
	if *permissions != (provider.BotPermissions{}) {
		t.Errorf("permissions = %+v, want empty", permissions)
	}
}

func TestUploadSARIF_IsUnsupported(t *testing.T) {
	t.Parallel()
	err := local.New().UploadSARIF(context.Background(), provider.SARIFUpload{})
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
