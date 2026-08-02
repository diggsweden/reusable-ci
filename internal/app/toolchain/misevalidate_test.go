// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestValidateMiseInstall_UnlockedConfigRequiresGithubTokenPresence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo = \"1.25\"\n")

	err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "false"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "false", GithubTokenPresent: true}); err != nil {
		t.Fatalf("github-token-present validation failed: %v", err)
	}
}

func TestValidateMiseInstall_UnlockedWithoutConfigAllowsNoToken(t *testing.T) {
	t.Parallel()

	if err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: t.TempDir(), Locked: "false"}); err != nil {
		t.Fatalf("ValidateMiseInstall: %v", err)
	}
}

func TestValidateMiseInstall_LockedRequiresMiseLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo = \"1.25\"\n")

	err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestValidateMiseInstall_LockedRequiresBackendRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, "mise.lock", "locked\n")
	writeToolchainFile(t, root, ".mise.toml", "[tools]\n\"pipx:yamllint\" = \"1.35.1\"\n")

	err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestValidateMiseInstall_LockedAcceptsPinnedBackendRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, "mise.lock", "locked\n")
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo = \"1.25\"\n\"go:github.com/x/y\" = \"1.0.0\"\n")

	if err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"}); err != nil {
		t.Fatalf("ValidateMiseInstall: %v", err)
	}
}

func TestValidateMiseInstall_LockedAcceptsRustupToolchainForCargo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeToolchainFile(t, root, "mise.lock", "locked\n")
	writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel = \"1.90.0\"\n")
	writeToolchainFile(t, root, ".mise.toml", "[tools]\n\"aqua:rust-lang/rustup\" = \"1.28.2\"\n\"cargo:cargo-auditable\" = \"0.7.0\"\n")

	if err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"}); err != nil {
		t.Fatalf("ValidateMiseInstall: %v", err)
	}
}

func TestValidateMiseInstall_RejectsInvalidLockedValue(t *testing.T) {
	t.Parallel()

	err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: t.TempDir(), Locked: "yes"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func writeToolchainFile(t *testing.T, root, rel, body string) {
	t.Helper()

	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { //nolint:gosec // test-owned path.
		t.Fatal(err)
	}
}
