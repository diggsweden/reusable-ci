// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ValidateMiseInstallInput drives `reusable-ci toolchain validate-mise-install`.
type ValidateMiseInstallInput struct {
	Root               string
	Locked             string
	GithubTokenPresent bool
}

// ValidateMiseInstall checks the setup-toolchain install mode before any token-bearing install work runs.
func ValidateMiseInstall(in ValidateMiseInstallInput) error {
	locked, err := parseMiseLocked(in.Locked)
	if err != nil {
		return err
	}

	root := in.Root
	if root == "" {
		root = "."
	}

	configs, hasConfig, err := readMiseConfigs(root)
	if err != nil {
		return err
	}

	if err = validateMiseInstallMode(root, locked, hasConfig, in.GithubTokenPresent); err != nil {
		return err
	}

	checks := []struct{ runtime, backend string }{{"uv", "pipx"}, {"go", "go"}, {toolRust, "cargo"}}
	for _, check := range checks {
		if err := requireRuntimeForLockedBackend(root, configs, locked, check.runtime, check.backend); err != nil {
			return err
		}
	}

	return nil
}

// validateMiseInstallMode enforces the locked-vs-token contract before installs run.
func validateMiseInstallMode(root string, locked, hasConfig, githubTokenPresent bool) error {
	if locked {
		hasLock, err := regularFileExists(root, "mise.lock")
		if err != nil {
			return err
		}

		if !hasLock {
			return fmt.Errorf("mise-locked=true requires a committed mise.lock: %w", errs.ErrValidation)
		}

		return nil
	}

	if hasConfig && !githubTokenPresent {
		return fmt.Errorf("setup-toolchain requires either mise-locked=true with mise.lock or mise-github-token: %w", errs.ErrValidation)
	}

	return nil
}

func parseMiseLocked(value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("mise-locked must be 'true' or 'false': %w", errs.ErrUsage)
	}
}

func requireRuntimeForLockedBackend(root string, configs []string, locked bool, runtime, backend string) error {
	if runtime == toolRust && rustupDeclared(configs) {
		hasRustToolchain, err := regularFileExists(root, "rust-toolchain.toml")
		if err != nil {
			return err
		}

		if hasRustToolchain {
			return nil
		}
	}

	if locked && backendDeclared(configs, backend) && !toolDeclared(configs, runtime) {
		return fmt.Errorf("mise-locked=true with %s: tools requires %s in .mise.toml, or aqua:rust-lang/rustup plus rust-toolchain.toml for Rust: %w", backend, runtime, errs.ErrValidation)
	}

	return nil
}

func readMiseConfigs(root string) ([]string, bool, error) {
	configs := make([]string, 0, 2)

	for _, rel := range []string{".mise.toml", "mise.toml"} {
		path := filepath.Join(root, rel)

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, false, fmt.Errorf("inspect %s: %w", path, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		body, err := os.ReadFile(path) //nolint:gosec // caller-selected repository root.
		if err != nil {
			return nil, false, fmt.Errorf("read %s: %w", path, err)
		}

		configs = append(configs, string(body))
	}

	return configs, len(configs) > 0, nil
}

func regularFileExists(root, rel string) (bool, error) {
	path := filepath.Join(root, rel)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspect %s: %w", path, err)
	}

	return info.Mode().IsRegular(), nil
}

func toolDeclared(configs []string, tool string) bool {
	return anyConfigMatches(configs, `(?m)^[\t ]*"?`+regexp.QuoteMeta(tool)+`"?[\t ]*=`)
}

func rustupDeclared(configs []string) bool {
	return toolDeclared(configs, "aqua:rust-lang/rustup")
}

func backendDeclared(configs []string, backend string) bool {
	pattern := `(?m)^[\t ]*"?` + regexp.QuoteMeta(backend) + `:`

	return anyConfigMatches(configs, pattern)
}

func anyConfigMatches(configs []string, pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, config := range configs {
		if re.MatchString(config) {
			return true
		}
	}

	return false
}
