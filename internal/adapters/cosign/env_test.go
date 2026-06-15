// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cosign

import (
	"slices"
	"testing"
)

func TestFilterEnv_KeepsRuntimeAndAllowedSecretsOnly(t *testing.T) {
	t.Parallel()

	environ := []string{
		"PATH=/usr/bin",                 // runtime → kept
		"HOME=/home/ci",                 // runtime → kept
		"COSIGN_KEY=-----BEGIN-----",    // allowed secret → kept
		"COSIGN_PASSWORD=hunter2",       // allowed secret → kept
		"FORGEJO_TOKEN=should-not-leak", // other secret → dropped
		"GPG_SIGNING_KEY=nope",          // other secret → dropped
		"REGISTRY_TOKEN=nope",           // other secret → dropped
		"malformed-no-equals",           // skipped
	}

	got := filterEnv(environ, map[string]bool{"COSIGN_KEY": true, "COSIGN_PASSWORD": true})

	want := []string{"PATH=/usr/bin", "HOME=/home/ci", "COSIGN_KEY=-----BEGIN-----", "COSIGN_PASSWORD=hunter2"}
	if !slices.Equal(got, want) {
		t.Errorf("filterEnv kept %v, want %v", got, want)
	}

	for _, kv := range got {
		if kv == "FORGEJO_TOKEN=should-not-leak" || kv == "GPG_SIGNING_KEY=nope" || kv == "REGISTRY_TOKEN=nope" {
			t.Errorf("secret leaked into isolated env: %q", kv)
		}
	}
}

func TestNewIsolated_SetsEnv(t *testing.T) {
	// No t.Parallel(): t.Setenv is incompatible with parallel tests.
	t.Setenv("COSIGN_KEY", "k")
	t.Setenv("SOME_OTHER_SECRET", "s")

	a := NewIsolated("COSIGN_KEY")
	if a.Env == nil {
		t.Fatal("NewIsolated should set Env")
	}

	for _, kv := range a.Env {
		if kv == "SOME_OTHER_SECRET=s" {
			t.Errorf("non-allowed secret present in isolated env: %q", kv)
		}
	}
}

func TestNew_InheritsEnv(t *testing.T) {
	t.Parallel()

	// Default adapter leaves Env nil → inherit (backwards compatible).
	if New().Env != nil {
		t.Error("New() must leave Env nil so the subprocess inherits the parent env")
	}
}
