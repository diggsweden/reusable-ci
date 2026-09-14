// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"github.com/pelletier/go-toml/v2"

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
	if err := validateToolchainEvidence(root, locked); err != nil {
		return err
	}

	if locked {
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

func requireRuntimeForLockedBackend(root string, configs map[string]any, locked bool, runtime, backend string) error {
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

func readMiseConfigs(root string) (map[string]any, bool, error) {
	configs := map[string]any{}
	hasConfig := false

	for _, rel := range []string{".mise.toml", "mise.toml"} {
		body, err := readToolchainFile(root, rel)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, false, err
		}

		var doc struct {
			Tools map[string]any `toml:"tools"`
		}
		if err := toml.Unmarshal(body, &doc); err != nil {
			return nil, false, fmt.Errorf("parse mise configuration %s: %w: %w", rel, err, errs.ErrMalformedInput)
		}

		for tool, spec := range doc.Tools {
			if _, exists := configs[tool]; exists {
				return nil, false, fmt.Errorf("mise tool %q is declared in both config files: %w", tool, errs.ErrValidation)
			}

			if err := immutableToolPin(rel, tool, spec); err != nil {
				return nil, false, err
			}

			configs[tool] = spec
		}

		hasConfig = true
	}

	if err := validateMiseLockPins(root); err != nil {
		return nil, false, err
	}

	return configs, hasConfig, nil
}

// immutableToolPin checks one declared tool version, in any of the shapes mise
// accepts: a bare string, a list of them, or a table carrying "version".
func immutableToolPin(file, tool string, spec any) error {
	switch value := spec.(type) {
	case string:
		if !immutableToolPinValue(value) {
			return fmt.Errorf("%s declares %s = %q, which is not an exact version: %w", file, tool, value, errs.ErrValidation)
		}

		return nil
	case []any:
		for _, item := range value {
			if err := immutableToolPin(file, tool, item); err != nil {
				return err
			}
		}

		return nil
	case map[string]any:
		version, ok := value["version"]
		if !ok {
			return fmt.Errorf("%s declares %s without a version: %w", file, tool, errs.ErrValidation)
		}

		return immutableToolPin(file, tool, version)
	default:
		return fmt.Errorf("%s declares %s with an unsupported version value: %w", file, tool, errs.ErrValidation)
	}
}

// validateMiseLockPins applies the same grammar to the lockfile when one is
// committed. A lock is meant to be the record of exactly what was installed, so
// a resolvable spelling in it is a lock that does not lock.
func validateMiseLockPins(root string) error {
	body, err := readToolchainFile(root, "mise.lock")
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	var doc struct {
		Tools map[string]any `toml:"tools"`
	}
	// The lock's format is mise's to define, and this repo only ever treated it
	// as evidence that a locked install is possible. A lock this cannot parse is
	// not evidence of a mutable pin, so it is left to mise rather than refused
	// here; what it does parse still has to be exact.
	if err := toml.Unmarshal(body, &doc); err != nil {
		return nil //nolint:nilerr // an unparsed lock is mise's contract, not a pin violation.
	}

	for tool, spec := range doc.Tools {
		if err := immutableToolPin("mise.lock", tool, spec); err != nil {
			return err
		}
	}

	return nil
}

func regularFileExists(root, rel string) (bool, error) {
	_, err := readToolchainFile(root, rel)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func readToolchainFile(dir, relative string) ([]byte, error) {
	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	info, err := root.Lstat(relative)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("toolchain evidence %s must be a nonlinked regular file: %w", relative, errs.ErrValidation)
	}

	return cliio.ReadFileInRoot(root, relative)
}

func toolDeclared(configs map[string]any, tool string) bool {
	_, exists := configs[tool]

	return exists
}

func rustupDeclared(configs map[string]any) bool {
	return toolDeclared(configs, "aqua:rust-lang/rustup")
}

func backendDeclared(configs map[string]any, backend string) bool {
	for tool := range configs {
		if strings.HasPrefix(tool, backend+":") {
			return true
		}
	}

	return false
}

func validateToolchainEvidence(root string, locked bool) error {
	if locked {
		if exists, err := regularFileExists(root, "mise.lock"); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("mise-locked=true requires a committed mise.lock: %w", errs.ErrValidation)
		}
	}

	return validateRustToolchain(root)
}

// Rustup documents <channel>[-<date>][-<host>] at
// https://rust-lang.github.io/rustup/concepts/toolchains.html. Require an exact
// stable version or a dated stable/beta/nightly channel. Host suffixes (including
// partial triples) are syntax-checked, not an inventory of native support.
var rustToolchainSelector = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+|stable|beta|nightly)(-[0-9]{4}-[0-9]{2}-[0-9]{2})?(-[A-Za-z_][A-Za-z0-9_]*(?:-[A-Za-z0-9_]+)*)?$`)

func validateRustToolchain(root string) error { //nolint:cyclop // optional evidence, TOML shape, immutable base, date and host syntax stay in one preflight.
	body, err := readToolchainFile(root, "rust-toolchain.toml")
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	var doc map[string]any
	if err = toml.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("parse rust-toolchain.toml: %w: %w", err, errs.ErrMalformedInput)
	}
	// TOML keys are case-sensitive; struct decoding also matches folded names.
	toolchain, _ := doc["toolchain"].(map[string]any)

	channel, ok := toolchain["channel"].(string)
	if !ok {
		return fmt.Errorf("rust-toolchain.toml requires a toolchain.channel string: %w", errs.ErrValidation)
	}

	selector := rustToolchainSelector.FindStringSubmatch(channel)
	if selector == nil {
		return fmt.Errorf("rust-toolchain.toml toolchain.channel must be an exact stable version or dated channel, optionally host-qualified: %w", errs.ErrValidation)
	}

	switch selector[1] {
	case "stable", "beta", "nightly":
		if selector[2] == "" {
			return fmt.Errorf("rust-toolchain.toml toolchain.channel must not use a moving alias: %w", errs.ErrValidation)
		}
	default:
		if !exactDownloadVersion(selector[1]) {
			return fmt.Errorf("rust-toolchain.toml toolchain.channel version must be exact MAJOR.MINOR.PATCH: %w", errs.ErrValidation)
		}
	}

	if selector[2] != "" {
		date, dateErr := time.Parse(time.DateOnly, selector[2][1:])
		if dateErr != nil || date.Year() == 0 {
			return fmt.Errorf("rust-toolchain.toml toolchain.channel date must be a valid YYYY-MM-DD: %w", errs.ErrValidation)
		}
	}
	// Bare release prerelease labels are not host selectors. Do not confuse a
	// legitimate hyphenated host triple with a SemVer prerelease suffix.
	switch strings.TrimRight(strings.TrimPrefix(selector[3], "-"), "0123456789") {
	case "alpha", "beta", "rc":
		return fmt.Errorf("rust-toolchain.toml toolchain.channel must not use a release prerelease: %w", errs.ErrValidation)
	}

	return nil
}
