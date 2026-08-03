// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

const (
	defaultMiseInstallAttempts = 3
	defaultMiseInstallDelay    = 10 * time.Second
)

const (
	// miseCommandInstall is the mise subcommand that installs declared tools.
	miseCommandInstall = "install"
	// toolRust is the mise runtime name for the Rust toolchain.
	toolRust = "rust"
)

// InstallMiseToolsInput drives `reusable-ci toolchain install-mise-tools`.
type InstallMiseToolsInput struct {
	Root          string
	Locked        string
	Tools         string
	RetryAttempts int
	RetryDelay    time.Duration
}

// InstallMiseTools runs the setup-toolchain mise install orchestration: sanitize
// child env, install declared backend runtimes first, install rustup/toolchain
// when declared, then install either the selected tool subset or the full config.
func InstallMiseTools(ctx context.Context, runner MiseRunner, out io.Writer, in InstallMiseToolsInput) error {
	if runner == nil {
		return fmt.Errorf("mise runner is required: %w", errs.ErrUsage)
	}

	locked, err := parseMiseLocked(in.Locked)
	if err != nil {
		return err
	}

	root := in.Root
	if root == "" {
		root = "."
	}

	configs, _, err := readMiseConfigs(root)
	if err != nil {
		return err
	}

	env, err := miseInstallEnv(os.Environ(), locked)
	if err != nil {
		return err
	}

	flags := miseInstallFlags(locked)

	for _, tool := range []string{"uv", "go", toolRust} {
		if toolDeclared(configs, tool) {
			if err = runMise(ctx, runner, env, out, append(append([]string{miseCommandInstall}, flags...), tool)...); err != nil {
				return err
			}
		}
	}

	env, err = installRustupToolchain(ctx, runner, env, out, root, configs, flags)
	if err != nil {
		return err
	}

	args := append([]string{miseCommandInstall}, flags...)
	args = append(args, strings.Fields(in.Tools)...)

	return runMiseWithRetry(ctx, runner, env, out, miseRetryAttempts(in.RetryAttempts), miseRetryDelay(in.RetryDelay), args...)
}

// installRustupToolchain installs rustup when declared and, when the repository
// pins a toolchain via rust-toolchain.toml, runs `rustup show` and prepends the
// rustup-managed cargo bin directory to PATH in the returned environment.
func installRustupToolchain(ctx context.Context, runner MiseRunner, env []string, out io.Writer, root string, configs, flags []string) ([]string, error) {
	if !rustupDeclared(configs) {
		return env, nil
	}

	if err := runMise(ctx, runner, env, out, append(append([]string{miseCommandInstall}, flags...), "aqua:rust-lang/rustup")...); err != nil {
		return nil, err
	}

	hasRustToolchain, err := regularFileExists(root, "rust-toolchain.toml")
	if err != nil {
		return nil, err
	}

	if !hasRustToolchain {
		return env, nil
	}

	execArgs := append(append([]string{"exec", "--no-deps"}, flags...), "aqua:rust-lang/rustup", "--", "rustup")
	if err = runMise(ctx, runner, env, out, append(execArgs, "show")...); err != nil {
		return nil, err
	}

	cargoPath, err := runner.Run(ctx, env, append(execArgs, "which", "cargo")...)
	if err != nil {
		return nil, fmt.Errorf("resolve rustup cargo path: %w", err)
	}

	cargoPath = strings.TrimSpace(cargoPath)
	if cargoPath == "" {
		return nil, fmt.Errorf("resolve rustup cargo path: empty output: %w", errs.ErrValidation)
	}

	return prependEnvPath(env, filepath.Dir(cargoPath)), nil
}

func runMise(ctx context.Context, runner MiseRunner, env []string, out io.Writer, args ...string) error {
	stdout, err := runner.Run(ctx, env, args...)
	if stdout != "" && out != nil {
		_, _ = fmt.Fprintln(out, stdout)
	}

	return err
}

func runMiseWithRetry(ctx context.Context, runner MiseRunner, env []string, out io.Writer, attempts int, delay time.Duration, args ...string) error {
	return retry.Run(ctx, nil, attempts, delay, func() error {
		return runMise(ctx, runner, env, out, args...)
	}, retry.WithBackoff(retry.Constant), retry.OnRetry(func(attempt, total int, _ time.Duration, _ error) {
		if out != nil {
			_, _ = fmt.Fprintf(out, "mise %s failed (attempt %d/%d); retrying...\n", strings.Join(args, " "), attempt, total)
		}
	}))
}

func miseInstallFlags(locked bool) []string {
	if locked {
		return []string{"--locked"}
	}

	return nil
}

func miseRetryAttempts(value int) int {
	if value <= 0 {
		return defaultMiseInstallAttempts
	}

	return value
}

func miseRetryDelay(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}

	if value == 0 {
		return defaultMiseInstallDelay
	}

	return value
}

func miseInstallEnv(env []string, locked bool) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	drop := map[string]bool{"GITHUB_TOKEN": true, "GH_TOKEN": true, "FORGEJO_TOKEN": true, "GITEA_TOKEN": true}
	dropIfEmpty := map[string]bool{
		"MISE_GITHUB_TOKEN": true, "MISE_FORGEJO_TOKEN": true,
		"PIP_INDEX_URL": true, "PIP_EXTRA_INDEX_URL": true, "PIP_FIND_LINKS": true, "PIP_CONFIG_FILE": true,
		"PIP_ARGS": true, "PIPX_ARGS": true, "PIPX_HOME": true, "PIPX_BIN_DIR": true, "PIPX_DEFAULT_PYTHON": true,
		"PIPX_PIP_ARGS": true, "MISE_PIPX_ARGS": true, "MISE_PIPX_PIP_ARGS": true,
		"UV_INDEX_URL": true, "UV_EXTRA_INDEX_URL": true, "UV_DEFAULT_INDEX": true,
	}

	out := make([]string, 0, len(env)+4)
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}

		if drop[name] || (dropIfEmpty[name] && strings.TrimSpace(value) == "") {
			continue
		}

		out = append(out, entry)
	}

	out = prependEnvPath(out,
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".cargo", "bin"),
	)

	if locked {
		// Deliberately disable mise's OWN verification layers in locked
		// mode: integrity is carried by the lockfile's per-tool checksums
		// (mise.lock pins exact archives) and by the mise binary itself
		// being sha256-pinned before it ever runs (validateInstallMiseInput).
		// LOCKED_VERIFY_PROVENANCE needs network calls to slsa-verifier
		// infrastructure the locked boundary must not depend on, and
		// PARANOID requires a GitHub token the signer deliberately lacks —
		// so both would trade a checksum guarantee we already have for a
		// network/credential dependency we must not add.
		overrides := map[string]string{"MISE_LOCKED_VERIFY_PROVENANCE": "0"}
		if !envHasNonEmpty(out, "MISE_GITHUB_TOKEN") {
			overrides["MISE_PARANOID"] = "0"
		}

		out = envWithOverrides(out, overrides)
	}

	return out, nil
}

func prependEnvPath(env []string, dirs ...string) []string {
	prefix := strings.Join(nonEmptyStrings(dirs), string(os.PathListSeparator))
	if prefix == "" {
		return env
	}

	out := env[:0]
	replaced := false

	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == "PATH" {
			if value != "" {
				value = prefix + string(os.PathListSeparator) + value
			} else {
				value = prefix
			}

			out = append(out, "PATH="+value)
			replaced = true

			continue
		}

		out = append(out, entry)
	}

	if !replaced {
		out = append(out, "PATH="+prefix)
	}

	return out
}

func envHasNonEmpty(env []string, name string) bool {
	for _, entry := range env {
		got, value, ok := strings.Cut(entry, "=")
		if ok && got == name && strings.TrimSpace(value) != "" {
			return true
		}
	}

	return false
}

func nonEmptyStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}

	return out
}
