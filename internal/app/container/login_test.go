// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
)

func readAuth(t *testing.T, path, registry string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test-controlled path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var doc struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	return doc.Auths[registry].Auth
}

func TestRegistryLogin_WritesAuthFile0600(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.json")

	err := appcontainer.RegistryLogin(io.Discard, appcontainer.RegistryLoginInput{
		Registry: "ghcr.io",
		Username: "alice",
		Password: "s3cret",
		AuthFile: path,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := readAuth(t, path, "ghcr.io"); got != base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) {
		t.Errorf("auth entry = %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth file perm = %o, want 600", perm)
	}
}

func TestRegistryLogin_PathPrecedence(t *testing.T) {
	// No t.Parallel(): this test mutates process env via t.Setenv.

	// REGISTRY_AUTH_FILE wins over DOCKER_CONFIG.
	authFile := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("REGISTRY_AUTH_FILE", authFile)
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	if err := appcontainer.RegistryLogin(io.Discard, appcontainer.RegistryLoginInput{
		Registry: "codeberg.org", Username: "bot", Password: "tok",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(authFile); err != nil {
		t.Errorf("expected write to $REGISTRY_AUTH_FILE: %v", err)
	}

	// An explicit AuthFile overrides even REGISTRY_AUTH_FILE.
	override := filepath.Join(t.TempDir(), "override.json")
	if err := appcontainer.RegistryLogin(io.Discard, appcontainer.RegistryLoginInput{
		Registry: "ghcr.io", Username: "u", Password: "p", AuthFile: override,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(override); err != nil {
		t.Errorf("explicit AuthFile not honored: %v", err)
	}
}

func TestRegistryLogout_RemovesOneKeepsOthersAndStays0600(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	for _, reg := range []string{"ghcr.io", "codeberg.org"} {
		if err := appcontainer.RegistryLogin(io.Discard, appcontainer.RegistryLoginInput{
			Registry: reg, Username: "u", Password: "p", AuthFile: path,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := appcontainer.RegistryLogout(io.Discard, appcontainer.RegistryLogoutInput{
		Registry: "ghcr.io", AuthFile: path,
	}); err != nil {
		t.Fatal(err)
	}

	if got := readAuth(t, path, "ghcr.io"); got != "" {
		t.Errorf("ghcr.io credential not removed: %q", got)
	}

	if got := readAuth(t, path, "codeberg.org"); got == "" {
		t.Error("codeberg.org credential was clobbered by logout")
	}

	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth file perm after logout = %o, want 600", perm)
	}
}

func TestRegistryLogout_IsIdempotent(t *testing.T) {
	t.Parallel()

	// Missing config file → no-op, no error, no file created.
	missing := filepath.Join(t.TempDir(), "config.json")
	if err := appcontainer.RegistryLogout(io.Discard, appcontainer.RegistryLogoutInput{
		Registry: "ghcr.io", AuthFile: missing,
	}); err != nil {
		t.Fatalf("logout on missing config should be a no-op, got %v", err)
	}

	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("logout on missing config must not create the file, stat err = %v", err)
	}
}
