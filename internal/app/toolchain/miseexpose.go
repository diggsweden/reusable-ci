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

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// MiseRunner is the mise subprocess surface needed by ExposeMiseTools.
type MiseRunner interface {
	Run(ctx context.Context, env []string, args ...string) (string, error)
}

// ExposeMiseToolsInput drives `reusable-ci toolchain expose-mise-tools`.
type ExposeMiseToolsInput struct {
	Root     string
	BinHome  string
	PathFile string
	Locked   string
}

// ExposeMiseTools makes mise-installed binaries reachable in later CI steps by
// appending their real bin directories to the runner path file and symlinking
// executable files into ~/.local/bin. It also exposes rustup-managed cargo/rustc
// bins when the repository declares rustup plus rust-toolchain.toml.
func ExposeMiseTools(ctx context.Context, runner MiseRunner, out io.Writer, in ExposeMiseToolsInput) error {
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

	binHome, err := resolveBinHome(in.BinHome)
	if err != nil {
		return err
	}

	if in.PathFile == "" {
		return fmt.Errorf("path-file is required: %w", errs.ErrUsage)
	}

	if err = os.MkdirAll(binHome, 0o755); err != nil { //nolint:gosec // tool bin dir read by later CI steps.
		return fmt.Errorf("create bin home %s: %w", binHome, err)
	}

	printInstalledTools(ctx, runner, out)

	if err = exposeMiseBinPaths(ctx, runner, binHome, in.PathFile); err != nil {
		return err
	}

	return exposeCargoBins(ctx, runner, root, binHome, in.PathFile, locked)
}

// printInstalledTools writes the mise installed-tools listing as a collapsed log group.
func printInstalledTools(ctx context.Context, runner MiseRunner, out io.Writer) {
	if out == nil {
		return
	}

	_, _ = fmt.Fprintln(out, "::group::setup-toolchain: installed tools")

	if installed, err := runner.Run(ctx, nil, "ls", "--installed"); err == nil && installed != "" {
		_, _ = fmt.Fprintln(out, installed)
	}

	_, _ = fmt.Fprintln(out, "::endgroup::")
}

// exposeMiseBinPaths exposes every mise bin-paths directory through the path file.
func exposeMiseBinPaths(ctx context.Context, runner MiseRunner, binHome, pathFile string) error {
	binPaths, err := runner.Run(ctx, nil, "bin-paths")
	if err != nil {
		return nil //nolint:nilerr // best-effort listing: when bin-paths fails, tools are simply not exposed.
	}

	for _, dir := range splitMiseLines(binPaths) {
		if err := exposeBinDir(dir, binHome, pathFile); err != nil {
			return err
		}
	}

	return nil
}

func resolveBinHome(value string) (string, error) {
	if value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "bin"), nil
}

func exposeCargoBins(ctx context.Context, runner MiseRunner, root, binHome, pathFile string, locked bool) error {
	configs, _, err := readMiseConfigs(root)
	if err != nil {
		return err
	}

	if !rustupDeclared(configs) {
		return nil
	}

	hasRustToolchain, err := regularFileExists(root, "rust-toolchain.toml")
	if err != nil {
		return err
	}

	if !hasRustToolchain {
		return nil
	}

	args := []string{"exec", "--no-deps"}
	env := []string(nil)

	if locked {
		args = append(args, "--locked")
		// Same rationale as miseinstalltools.go: the lockfile checksums plus
		// the sha256-pinned mise binary carry integrity; mise's own
		// provenance/paranoid layers would add network/credential
		// dependencies the locked boundary must not have.
		env = envWithOverrides(os.Environ(), map[string]string{
			"MISE_LOCKED_VERIFY_PROVENANCE": "0",
			"MISE_PARANOID":                 "0",
		})
	}

	args = append(args, "aqua:rust-lang/rustup", "--", "rustup", "which", "cargo")

	cargoPath, err := runner.Run(ctx, env, args...)
	if err != nil {
		return fmt.Errorf("resolve rustup cargo path: %w", err)
	}

	cargoPath = strings.TrimSpace(cargoPath)
	if cargoPath == "" {
		return fmt.Errorf("resolve rustup cargo path: empty output: %w", errs.ErrValidation)
	}

	return exposeBinDir(filepath.Dir(cargoPath), binHome, pathFile)
}

func exposeBinDir(dir, binHome, pathFile string) error {
	if dir == "" {
		return nil
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil //nolint:nilerr // missing or non-directory bin paths are skipped, not fatal.
	}

	if err = appendPathFile(pathFile, dir); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read bin directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if err = exposeExecutable(dir, binHome, entry); err != nil {
			return err
		}
	}

	return nil
}

// exposeExecutable symlinks one executable regular file from dir into binHome.
func exposeExecutable(dir, binHome string, entry os.DirEntry) error {
	path := filepath.Join(dir, entry.Name())

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil //nolint:nilerr // unstatable or non-executable entries are skipped, not fatal.
	}

	link := filepath.Join(binHome, entry.Name())
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace symlink %s: %w", link, err)
	}

	if err := os.Symlink(path, link); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", link, path, err)
	}

	return nil
}

func appendPathFile(pathFile, dir string) error {
	file, err := os.OpenFile(pathFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // CI runner path file chosen by caller.
	if err != nil {
		return fmt.Errorf("open path file %s: %w", pathFile, err)
	}

	defer func() { _ = file.Close() }()

	if _, err := fmt.Fprintln(file, dir); err != nil {
		return fmt.Errorf("append %s to path file %s: %w", dir, pathFile, err)
	}

	return nil
}

func splitMiseLines(value string) []string {
	var out []string

	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}

	return out
}

func envWithOverrides(env []string, overrides map[string]string) []string {
	out := env[:0]
	for _, item := range env {
		name, _, ok := strings.Cut(item, "=")
		if ok {
			if _, replace := overrides[name]; replace {
				continue
			}
		}

		out = append(out, item)
	}

	for name, value := range overrides {
		out = append(out, name+"="+value)
	}

	return out
}
