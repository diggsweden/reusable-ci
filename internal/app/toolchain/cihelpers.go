// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// CacheDiscriminatorInput drives `toolchain cache-discriminator`.
type CacheDiscriminatorInput struct {
	Tools           string
	InstallDevTools string
	ExtraCachePaths string
}

// CacheDiscriminator returns the short stable cache-key discriminator used by
// setup-toolchain for selected tool subsets and extra cache paths.
func CacheDiscriminator(in CacheDiscriminatorInput) string {
	sum := sha256.Sum256([]byte(in.Tools + "|" + in.InstallDevTools + "|" + in.ExtraCachePaths))

	return hex.EncodeToString(sum[:])[:16]
}

// SetupMiseEnvInput drives `toolchain setup-mise-env`.
type SetupMiseEnvInput struct {
	Cache      string
	BinHome    string
	PathFile   string
	EnvFile    string
	RunnerTemp string
}

// SetupMiseEnv writes the runner path/env file entries needed after installing
// mise and, in cache=false mode, isolates mise's mutable data/cache/state trees
// under the runner temp directory.
func SetupMiseEnv(in SetupMiseEnvInput) error {
	cacheEnabled, err := parseSetupCache(in.Cache)
	if err != nil {
		return err
	}

	if in.PathFile == "" {
		return fmt.Errorf("setup-mise-env: path-file is required: %w", errs.ErrUsage)
	}

	if in.EnvFile == "" {
		return fmt.Errorf("setup-mise-env: env-file is required: %w", errs.ErrUsage)
	}

	binHome, err := resolveBinHome(in.BinHome)
	if err != nil {
		return err
	}

	if err = setupMiseBinPaths(in.PathFile, binHome); err != nil {
		return err
	}

	temp := defaultRunnerTemp(in.RunnerTemp)

	configDir := filepath.Join(temp, "setup-toolchain-mise-config")
	if err = os.MkdirAll(configDir, 0o755); err != nil { //nolint:gosec // isolated mise config dir read by later CI steps.
		return fmt.Errorf("create mise config dir %s: %w", configDir, err)
	}

	entries := []string{"MISE_CONFIG_DIR=" + configDir}

	if !cacheEnabled {
		base := filepath.Join(temp, "setup-toolchain-mise-data")
		for _, name := range []string{"data", "cache", "state"} {
			if err = os.MkdirAll(filepath.Join(base, name), 0o755); err != nil { //nolint:gosec // isolated mise tree read by later CI steps.
				return fmt.Errorf("create mise %s dir: %w", name, err)
			}
		}

		entries = append(entries,
			"MISE_DATA_DIR="+filepath.Join(base, "data"),
			"MISE_CACHE_DIR="+filepath.Join(base, "cache"),
			"MISE_STATE_DIR="+filepath.Join(base, "state"),
		)
	}

	return appendEnvFile(in.EnvFile, entries...)
}

// setupMiseBinPaths creates the bin/data homes and exposes the tool bin
// directories through the runner path file.
func setupMiseBinPaths(pathFile, binHome string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	if err := os.MkdirAll(binHome, 0o755); err != nil { //nolint:gosec // tool bin dir read by later CI steps.
		return fmt.Errorf("create bin home %s: %w", binHome, err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "mise"), 0o755); err != nil { //nolint:gosec // mise data home read by later CI steps.
		return fmt.Errorf("create mise data home: %w", err)
	}

	if err := appendPathFile(pathFile, binHome); err != nil {
		return err
	}

	return appendPathFile(pathFile, filepath.Join(home, ".cargo", "bin"))
}

// InstallChangelogRendererInput drives `toolchain install-changelog-renderer`.
type InstallChangelogRendererInput struct {
	Backend          string
	GitChglogVersion string
	GitCliffVersion  string
	Mise             InstallMiseInput
	BinHome          string
	PathFile         string
	RunnerTemp       string
	RunID            string
}

// InstallChangelogRenderer installs the selected changelog renderer into an
// isolated mise tree and exposes the actual binary through ~/.local/bin.
func InstallChangelogRenderer(ctx context.Context, runner MiseRunner, out io.Writer, in InstallChangelogRendererInput) error {
	if err := validateChangelogRendererInput(runner, in); err != nil {
		return err
	}

	binHome, err := ensureBinHome(in.BinHome)
	if err != nil {
		return err
	}

	if _, err = InstallMise(ctx, nil, out, in.Mise); err != nil {
		return err
	}

	selector, bin, version, err := changelogRendererSelector(in)
	if err != nil {
		return err
	}

	env, err := prepareChangelogMiseEnv(in, binHome)
	if err != nil {
		return err
	}

	tool := selector + "@" + version
	if err = runMise(ctx, runner, env, out, "--no-config", "install", tool); err != nil {
		return err
	}

	candidate, err := resolveChangelogBinary(ctx, runner, env, tool, bin)
	if err != nil {
		return err
	}

	link, err := linkChangelogBinary(candidate, binHome, bin)
	if err != nil {
		return err
	}

	if err = appendPathFile(in.PathFile, binHome); err != nil {
		return err
	}

	return printToolVersion(ctx, out, link)
}

// validateChangelogRendererInput checks the required runner and path-file inputs.
func validateChangelogRendererInput(runner MiseRunner, in InstallChangelogRendererInput) error {
	if runner == nil {
		return fmt.Errorf("mise runner is required: %w", errs.ErrUsage)
	}

	if in.PathFile == "" {
		return fmt.Errorf("install-changelog-renderer: path-file is required: %w", errs.ErrUsage)
	}

	return nil
}

// ensureBinHome resolves the tool bin home and creates it.
func ensureBinHome(binHome string) (string, error) {
	resolved, err := resolveBinHome(binHome)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(resolved, 0o755); err != nil { //nolint:gosec // tool bin dir read by later CI steps.
		return "", fmt.Errorf("create bin home %s: %w", resolved, err)
	}

	return resolved, nil
}

// prepareChangelogMiseEnv creates the isolated prepare-mise tree and returns
// the environment pointing mise at it.
func prepareChangelogMiseEnv(in InstallChangelogRendererInput, binHome string) ([]string, error) {
	prepareDir := filepath.Join(defaultRunnerTemp(in.RunnerTemp), "reusable-ci-prepare-mise-"+defaultRunID(in.RunID))
	if err := os.RemoveAll(prepareDir); err != nil {
		return nil, fmt.Errorf("remove prior prepare mise dir %s: %w", prepareDir, err)
	}

	for _, name := range []string{"cache", "config", "data", "state"} {
		if err := os.MkdirAll(filepath.Join(prepareDir, name), 0o755); err != nil { //nolint:gosec // isolated mise prepare tree read by later CI steps.
			return nil, fmt.Errorf("create prepare mise %s dir: %w", name, err)
		}
	}

	return prepareMiseEnv(os.Environ(), binHome, prepareDir), nil
}

// resolveChangelogBinary locates the installed renderer binary inside the mise tool dir.
func resolveChangelogBinary(ctx context.Context, runner MiseRunner, env []string, tool, bin string) (string, error) {
	installDir, err := runner.Run(ctx, env, "--no-config", "where", tool)
	if err != nil {
		return "", fmt.Errorf("resolve %s install dir: %w", bin, err)
	}

	installDir = strings.TrimSpace(installDir)
	if installDir == "" {
		return "", fmt.Errorf("resolve %s install dir: empty output: %w", bin, errs.ErrValidation)
	}

	return findMiseToolBinary(installDir, bin)
}

// linkChangelogBinary replaces the bin-home symlink with one pointing at candidate.
func linkChangelogBinary(candidate, binHome, bin string) (string, error) {
	link := filepath.Join(binHome, bin)
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("replace symlink %s: %w", link, err)
	}

	if err := os.Symlink(candidate, link); err != nil {
		return "", fmt.Errorf("symlink %s -> %s: %w", link, candidate, err)
	}

	return link, nil
}

func parseSetupCache(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("setup-mise-env: cache must be true or false: %s: %w", value, errs.ErrUsage)
	}
}

func defaultRunnerTemp(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}

	if env := os.Getenv("RUNNER_TEMP"); strings.TrimSpace(env) != "" {
		return env
	}

	return os.TempDir()
}

func appendEnvFile(path string, entries ...string) error {
	envFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // CI runner env file chosen by caller.
	if err != nil {
		return fmt.Errorf("open env file %s: %w", path, err)
	}

	defer func() { _ = envFile.Close() }()

	for _, entry := range entries {
		if _, err := fmt.Fprintln(envFile, entry); err != nil {
			return fmt.Errorf("append env entry to %s: %w", path, err)
		}
	}

	return nil
}

func defaultRunID(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}

	return strconv.Itoa(os.Getpid())
}

func prepareMiseEnv(env []string, binHome, prepareDir string) []string {
	out := envWithOverrides(env, map[string]string{
		"MISE_CACHE_DIR":  filepath.Join(prepareDir, "cache"),
		"MISE_CONFIG_DIR": filepath.Join(prepareDir, "config"),
		"MISE_DATA_DIR":   filepath.Join(prepareDir, "data"),
		"MISE_STATE_DIR":  filepath.Join(prepareDir, "state"),
	})

	return prependEnvPath(out, binHome)
}

func changelogRendererSelector(in InstallChangelogRendererInput) (string, string, string, error) {
	switch strings.TrimSpace(in.Backend) {
	case "git-chglog":
		return "aqua:git-chglog/git-chglog", "git-chglog", in.GitChglogVersion, requireVersion("git-chglog", in.GitChglogVersion)
	case "git-cliff":
		return "aqua:orhun/git-cliff", "git-cliff", in.GitCliffVersion, requireVersion("git-cliff", in.GitCliffVersion)
	default:
		return "", "", "", fmt.Errorf("install-changelog-renderer: backend must be git-chglog or git-cliff: %s: %w", in.Backend, errs.ErrValidation)
	}
}

func requireVersion(name, version string) error {
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("install-changelog-renderer: %s version is required: %w", name, errs.ErrUsage)
	}

	return nil
}

func findMiseToolBinary(installDir, bin string) (string, error) {
	candidate := filepath.Join(installDir, bin)
	if executableRegularFile(candidate) {
		return candidate, nil
	}

	matches, err := filepath.Glob(filepath.Join(installDir, "*", bin))
	if err != nil {
		return "", fmt.Errorf("search %s for %s: %w", installDir, bin, err)
	}

	for _, path := range matches {
		if executableRegularFile(path) {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find %s in %s: %w", bin, installDir, errs.ErrMissingInput)
}

func executableRegularFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func printToolVersion(ctx context.Context, out io.Writer, bin string) error {
	cmd := safeexec.Command(ctx, bin, "--version")

	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s --version: %w", bin, safeexec.WrapError(err, bin, "--version"))
	}

	if out != nil && len(stdout) > 0 {
		_, _ = out.Write(stdout)
		if !strings.HasSuffix(string(stdout), "\n") {
			_, _ = fmt.Fprintln(out)
		}
	}

	return nil
}
