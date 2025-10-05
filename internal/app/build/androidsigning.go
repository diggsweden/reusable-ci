// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

func validateAndroidCredentials(in AndroidReleaseBuildInput) error {
	if !in.EnableSigning {
		return nil
	}

	for _, credential := range []struct{ name, value string }{
		{"ANDROID_KEYSTORE_PASSWORD", in.KeystorePassword},
		{"ANDROID_KEY_ALIAS", in.KeyAlias},
		{"ANDROID_KEY_PASSWORD", in.KeyPassword},
	} {
		if credential.value == "" {
			return fmt.Errorf("%s is required when signing: %w", credential.name, errs.ErrPermissionDenied)
		}

		if strings.ContainsRune(credential.value, 0) {
			return fmt.Errorf("%s must be NUL-free: %w", credential.name, errs.ErrValidation)
		}
	}

	if strings.TrimSpace(in.KeyAlias) == "" {
		return fmt.Errorf("ANDROID_KEY_ALIAS must not be whitespace-only: %w", errs.ErrValidation)
	}

	return nil
}

func openAndroidBuildFile(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", name, err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a nonlinked regular file: %w", name, errs.ErrValidation)
	}

	if info.Mode().Perm()&0o444 == 0 {
		return nil, fmt.Errorf("%s must be readable: %w", name, errs.ErrPermissionDenied)
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}

	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.Join(fmt.Errorf("%s changed while opening: %w", name, errs.ErrValidation), err, file.Close())
	}

	return file, nil
}

func checkAndroidMetadata(root *os.Root) error {
	file, err := openAndroidBuildFile(root, "gradle.properties")
	if errors.Is(err, os.ErrNotExist) {
		return nil // Native Gradle metadata may legitimately be computed at build time.
	}

	if err != nil {
		return err
	}

	return file.Close()
}

func checkBuildWritableRoot(root *os.Root) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	// This catches known mode refusals without a write probe. ACLs and later I/O
	// failures still belong to staging, not a promise of preflight atomicity.
	if info.Mode().Perm()&0o222 == 0 || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("build staging destination must be writable and searchable: %w", errs.ErrPermissionDenied)
	}

	return nil
}

func androidKeystoreTempDir(chosen, project string) (string, error) {
	if chosen == "" {
		var err error

		chosen, err = mobileTempRoot()
		if err != nil {
			return "", err
		}
	}

	dir, err := filepath.Abs(chosen)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(project, dir)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return "", fmt.Errorf("android keystore scratch must be outside the project: %w", errs.ErrValidation)
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open Android keystore scratch: %w", err)
	}

	if err := errors.Join(checkBuildWritableRoot(root), root.Close()); err != nil {
		return "", err
	}

	return dir, nil
}

// Keep the created file open until cleanup so an unlinked inode cannot be reused
// and mistaken for ours. Stable caller replacements are refused, not deleted.
// This identity check is not a sandbox against a hostile concurrent build.
func createAndroidProperties(root *os.Root, body []byte) (func() error, error) { //nolint:cyclop // create-only write with all-exit, identity-checked cleanup.
	const name = "secrets.properties"

	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}

	info, statErr := file.Stat()

	cleanup := func() error {
		current, err := root.Lstat(name)
		switch {
		case errors.Is(err, os.ErrNotExist):
			err = nil
		case err != nil:
		case info == nil || !current.Mode().IsRegular() || !os.SameFile(info, current):
			err = fmt.Errorf("secrets.properties identity changed; preserving replacement: %w", errs.ErrValidation)
		default:
			err = root.Remove(name)
		}

		if err = errors.Join(err, file.Close()); err != nil {
			return fmt.Errorf("clean injected secrets.properties: %w", err)
		}

		return nil
	}
	if statErr != nil {
		return nil, errors.Join(statErr, cleanup())
	}

	if _, err := file.Write(body); err != nil {
		return nil, errors.Join(err, cleanup())
	}

	return cleanup, nil
}
