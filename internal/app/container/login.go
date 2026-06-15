// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// RegistryLoginInput drives RegistryLogin.
type RegistryLoginInput struct {
	Registry string
	Username string
	Password string
	// AuthFile overrides where the credentials are written. Empty resolves the
	// tool-neutral default (see authFilePath).
	AuthFile string
}

// RegistryLogin writes registry credentials to the shared OCI auth config in
// the {"auths":{…}} format, the node-less, forge-neutral replacement for
// docker/login-action. The same file authenticates docker, podman/buildah,
// skopeo, and cosign.
//
// Security: the password is never placed in argv (callers resolve it from
// stdin/file/env); it is never logged or returned in an error. The file is
// written 0600 and its parent created 0700 — base64 in the config is encoding,
// not encryption, so the permissions are the protection. Existing credentials
// for other registries in the file are preserved.
func RegistryLogin(w io.Writer, in RegistryLoginInput) error { //nolint:varnamelen // idiomatic short name (io conventions).
	path, err := authFilePath(in.AuthFile)
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(path) //nolint:gosec // operator-controlled auth path.
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read auth config %s: %w", path, err)
	}

	merged, err := container.MergeAuth(existing, in.Registry, in.Username, in.Password)
	if err != nil {
		return err // domain error already redaction-safe (no password echoed)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create auth config dir: %w", err)
	}

	if err := cliio.WriteFile(path, merged, 0o600); err != nil {
		return fmt.Errorf("write auth config %s: %w", path, err)
	}

	_, _ = fmt.Fprintf(w, "Logged in to %s as %s → %s\n", in.Registry, in.Username, path)

	return nil
}

// authFilePath resolves where credentials are written so BOTH docker and podman
// pick them up. Precedence mirrors the containers-auth / docker-config search
// order: an explicit override, then $REGISTRY_AUTH_FILE (podman/buildah), then
// $DOCKER_CONFIG/config.json (docker), then ~/.docker/config.json — the last of
// which podman also reads as a fallback.
func authFilePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}

	if f := os.Getenv("REGISTRY_AUTH_FILE"); f != "" {
		return f, nil
	}

	if d := os.Getenv("DOCKER_CONFIG"); d != "" {
		return filepath.Join(d, "config.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for auth config: %w", errs.ErrInvalidConfig)
	}

	return filepath.Join(home, ".docker", "config.json"), nil
}
