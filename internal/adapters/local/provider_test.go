// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package local_test

import (
	"context"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/local"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestProvider_Name(t *testing.T) {
	t.Parallel()

	if got := local.New().Name(); got != provider.ForgeLocal {
		t.Errorf("Name = %q", got)
	}
}

func TestResolveContext_FromInjectedEnv(t *testing.T) {
	t.Parallel()

	envFunc := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	p := &local.Provider{Env: envFunc(map[string]string{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		"CI_REF_NAME": "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"CI_COMMIT":   "abcdef0123456789",
		"CI_BRANCH":   "main",
		"CI_REPO":     "owner/repo",
	})}

	evt, _ := p.ResolveContext(context.Background())
	if evt.ForgeAPI != provider.ForgeLocal {
		t.Errorf("Platform = %q", evt.ForgeAPI)
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

	if evt.ForgeAPI != provider.ForgeLocal {
		t.Errorf("Platform = %q", evt.ForgeAPI)
	}
	// Other fields all empty / zero — that's the contract.
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

// TestProvider_DoesNotImplementUnsupportedRoles pins the deliberate
// LSP design: local.Provider does NOT satisfy the role interfaces it
// has no meaningful implementation for. CLI surfaces that need them
// gate on platform via deps.Require*() and surface a typed error,
// rather than a runtime ErrUnsupported deep in the call stack.
func TestProvider_DoesNotImplementUnsupportedRoles(t *testing.T) {
	t.Parallel()

	p := local.New() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if _, ok := any(p).(provider.TokenValidator); ok {
		t.Error("local.Provider should not implement TokenValidator")
	}

	if _, ok := any(p).(provider.ReleaseCreator); ok {
		t.Error("local.Provider should not implement ReleaseCreator")
	}

	if _, ok := any(p).(provider.ReleaseAssetUploader); ok {
		t.Error("local.Provider should not implement ReleaseAssetUploader")
	}

	if _, ok := any(p).(provider.SARIFUploader); ok {
		t.Error("local.Provider should not implement SARIFUploader")
	}
}
