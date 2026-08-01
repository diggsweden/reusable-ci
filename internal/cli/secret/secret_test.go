// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package secret_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestResolve_FilePathWins(t *testing.T) {
	fsys := testfs.NewReal(t)
	path := fsys.WriteFile("token.txt", []byte("ghp_fileval\n"))
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "ghp_envval")

	got, err := secret.Resolve(path, "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "ghp_fileval" {
		t.Errorf("Resolve = %q, want file value with trailing newline trimmed", got)
	}
}

func TestResolve_EnvFallback(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "ghp_envval\n")

	got, err := secret.Resolve("", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "ghp_envval" {
		t.Errorf("Resolve = %q, want env value with trailing newline trimmed", got)
	}
}

func TestResolve_EmptyWhenNeitherSet(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("RELEASE_TOKEN", "")

	got, err := secret.Resolve("", "RELEASE_TOKEN")
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Errorf("Resolve = %q, want empty string", got)
	}
}

func TestResolve_MissingFileSurfacesError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")

	_, err := secret.Resolve(missing, "")
	if err == nil || !strings.Contains(err.Error(), "read secret from") {
		t.Errorf("err = %v, want wrapped read-secret error", err)
	}
}

func TestResolve_EmptyEnvVarNameReturnsEmpty(t *testing.T) {
	got, err := secret.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}

	if got != "" {
		t.Errorf("Resolve = %q, want empty", got)
	}
}
