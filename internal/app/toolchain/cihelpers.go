// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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
	hash := sha256.New()
	for _, value := range []string{in.Tools, in.InstallDevTools, in.ExtraCachePaths} {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = io.WriteString(hash, value)
	}

	return hex.EncodeToString(hash.Sum(nil))[:16]
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
// Runner export files are append-only: identical reruns append identical entries
// rather than rewriting exports left by an earlier command in the same step.
// Known local obstacles refuse before any creation or append. Later I/O failures
// may leave created directories or earlier appends; this is not a transaction.
func SetupMiseEnv(in SetupMiseEnvInput) error { //nolint:cyclop // plan both runner files and every directory before effects.
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

	binHome, err = resolveMiseLocalPath(binHome, true)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	home, err = resolveMiseLocalPath(home, true)
	if err != nil {
		return err
	}

	temp, err := defaultRunnerTemp(in.RunnerTemp)
	if err != nil {
		return err
	}

	temp, err = resolveMiseLocalPath(temp, false)
	if err != nil {
		return err
	}

	configDir := filepath.Join(temp, "setup-toolchain-mise-config")
	dirs := []string{binHome, filepath.Join(home, ".local", "share", "mise"), configDir}
	entries := []string{"MISE_CONFIG_DIR=" + configDir}

	if !cacheEnabled {
		base := filepath.Join(temp, "setup-toolchain-mise-data")
		for _, name := range []string{"data", "cache", "state"} {
			dirs = append(dirs, filepath.Join(base, name))
		}

		entries = append(entries,
			"MISE_DATA_DIR="+filepath.Join(base, "data"),
			"MISE_CACHE_DIR="+filepath.Join(base, "cache"),
			"MISE_STATE_DIR="+filepath.Join(base, "state"),
		)
	}

	pathFile, err := resolveMiseLocalPath(in.PathFile, false)
	if err != nil {
		return err
	}

	envFile, err := resolveMiseLocalPath(in.EnvFile, false)
	if err != nil {
		return err
	}

	if err = preflightMiseLocalPaths(dirs, []string{pathFile, envFile}); err != nil {
		return err
	}

	for _, dir := range dirs {
		root, createErr := pathsafe.MkdirRoot(dir, 0o755)
		if createErr != nil {
			return fmt.Errorf("create mise directory %s: %w", dir, createErr)
		}

		_ = root.Close()
	}

	if err = appendPathFile(pathFile, binHome); err != nil {
		return err
	}

	if err = appendPathFile(pathFile, filepath.Join(home, ".cargo", "bin")); err != nil {
		return err
	}

	return appendEnvFile(envFile, entries...)
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
// Local preflight precedes installation/reset. Later failures retain completed
// installation, scratch reset, links and appends; no rollback is promised.
func InstallChangelogRenderer(ctx context.Context, runner MiseRunner, out io.Writer, in InstallChangelogRendererInput) error { //nolint:cyclop // validate mandatory pins before starting the existing install/expose sequence.
	if err := validateChangelogRendererInput(runner, in); err != nil {
		return err
	}

	selector, bin, version, err := changelogRendererSelector(in)
	if err != nil {
		return err
	}

	if err = validateInstallMiseInput(in.Mise); err != nil {
		return err
	}

	in, prepareDir, err := preflightChangelogPaths(in, bin)
	if err != nil {
		return err
	}

	arch, _, err := miseArchiveArchAndSHA(in.Mise)
	if err != nil {
		return err
	}

	if _, _, err = miseArchiveURL(in.Mise, arch); err != nil {
		return err
	}

	binHome, err := ensureBinHome(in.BinHome)
	if err != nil {
		return err
	}

	misePath, err := InstallMise(ctx, nil, out, in.Mise)
	if err != nil {
		return err
	}

	// Pin the runner to the binary we JUST installed: a bare "mise" is
	// resolved against the parent process PATH, which cannot contain the
	// fresh ~/.local/bin entry (the path-file export only reaches LATER
	// steps). Without this, the verb only works on runner images that
	// happen to pre-ship mise — the failure mode nanolinter's release hit.
	adoptInstalledMise(runner, misePath)

	env, err := prepareChangelogMiseEnv(prepareDir, binHome)
	if err != nil {
		return err
	}

	installRoot, err := changelogInstallRoot(prepareDir)
	if err != nil {
		return err
	}

	tool := selector + "@" + version
	if err = runMise(ctx, runner, env, out, "--no-config", "install", tool); err != nil {
		return err
	}

	candidate, err := resolveChangelogBinary(ctx, runner, env, tool, bin, installRoot)
	if err != nil {
		return err
	}

	link, err := publishChangelogBinary(candidate, binHome, bin, in.PathFile)
	if err != nil {
		return err
	}

	return printToolVersion(ctx, out, link)
}

// binSetter is the optional capability an installed-binary-aware runner
// exposes (the mise adapter does); fakes without it are simply left alone.
type binSetter interface{ SetBin(bin string) }

// adoptInstalledMise pins the runner to the just-installed mise binary when
// the runner supports it and the installer reported a concrete path.
func adoptInstalledMise(runner MiseRunner, misePath string) {
	if misePath == "" {
		return
	}

	if setter, ok := runner.(binSetter); ok {
		setter.SetBin(misePath)
	}
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

func preflightChangelogPaths(in InstallChangelogRendererInput, bin string) (InstallChangelogRendererInput, string, error) { //nolint:cyclop // resolve once, then jointly check install, export and reset authority.
	prepareDir, err := changelogPrepareDir(in.RunID, in.RunnerTemp)
	if err != nil {
		return in, "", err
	}

	in.BinHome, err = resolveBinHome(in.BinHome)
	if err != nil {
		return in, "", err
	}

	in.BinHome, err = resolveMiseLocalPath(in.BinHome, true)
	if err != nil {
		return in, "", err
	}

	in.Mise.DestDir, err = resolveMiseDestDir(in.Mise.DestDir)
	if err != nil {
		return in, "", err
	}

	in.Mise.DestDir, err = resolveMiseLocalPath(in.Mise.DestDir, false)
	if err != nil {
		return in, "", err
	}

	in.PathFile, err = resolveMiseLocalPath(in.PathFile, false)
	if err != nil {
		return in, "", err
	}

	for _, path := range []string{in.PathFile, in.BinHome, in.Mise.DestDir} {
		if misePathWithin(path, prepareDir) {
			return in, "", fmt.Errorf("prepare reset overlaps caller path %s: %w", path, errs.ErrValidation)
		}
	}

	installPath := filepath.Join(in.Mise.DestDir, "mise")

	selectedPath := filepath.Join(in.BinHome, bin)
	if misePathWithin(in.PathFile, selectedPath) || misePathWithin(prepareDir, installPath) || misePathWithin(in.BinHome, installPath) ||
		misePathWithin(prepareDir, selectedPath) || misePathWithin(in.Mise.DestDir, selectedPath) {
		return in, "", fmt.Errorf("changelog paths overlap an executable destination: %w", errs.ErrValidation)
	}

	if err = preflightMiseLocalPaths([]string{in.BinHome, in.Mise.DestDir, prepareDir}, []string{in.PathFile, installPath}); err != nil {
		return in, "", err
	}

	selectedLeaf, err := os.Lstat(selectedPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return in, "", fmt.Errorf("inspect selected changelog destination: %w", err)
	}

	if selectedLeaf != nil && !selectedLeaf.Mode().IsRegular() && selectedLeaf.Mode()&os.ModeSymlink == 0 {
		return in, "", fmt.Errorf("selected changelog destination must be a regular file or symlink: %w", errs.ErrValidation)
	}

	pathInfo, pathErr := os.Stat(in.PathFile)

	selectedInfo, selectedErr := os.Stat(selectedPath)
	if selectedErr == nil && (selectedInfo.IsDir() || (pathErr == nil && os.SameFile(pathInfo, selectedInfo))) {
		return in, "", fmt.Errorf("selected changelog destination is a directory or aliases PATH: %w", errs.ErrValidation)
	}

	return in, prepareDir, nil
}

// PATH list separators only matter for exported paths, not ENV values or local
// output filenames. Validate raw spelling too, before Abs can erase components.
func resolveMiseLocalPath(path string, pathEntry bool) (string, error) {
	if strings.ContainsAny(path, "\x00\r\n\t") || (pathEntry && !validMisePathSpelling(path)) {
		return "", fmt.Errorf("invalid mise local path %q: %w", path, errs.ErrValidation)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if strings.ContainsAny(abs, "\x00\r\n\t") || (pathEntry && !validMisePathSpelling(abs)) {
		return "", fmt.Errorf("invalid resolved mise local path %q: %w", abs, errs.ErrValidation)
	}

	return abs, nil
}

func misePathWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// A missing runner-file parent is valid only if a planned MkdirAll creates it.
// OpenRoot checks every existing ancestor without following links, even when a
// later component is missing. This preflight performs no filesystem mutations.
func preflightMiseLocalPaths(dirs, files []string) error { //nolint:cyclop,gocognit // both path spelling and existing-file identity participate in the joint plan.
	for _, dir := range dirs {
		root, err := pathsafe.OpenRoot(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect planned mise directory %s: %w", dir, err)
		}

		if root != nil {
			_ = root.Close()
		}
	}

	infos := make([]os.FileInfo, len(files))
	for index, path := range files {
		parentPlanned := false

		for _, dir := range dirs {
			if misePathWithin(dir, path) {
				return fmt.Errorf("runner/install file overlaps planned directory %s: %w", path, errs.ErrValidation)
			}

			parentPlanned = parentPlanned || misePathWithin(dir, filepath.Dir(path))
		}

		root, err := pathsafe.OpenRoot(filepath.Dir(path))
		if err != nil && (!parentPlanned || !errors.Is(err, os.ErrNotExist)) {
			return fmt.Errorf("open runner/install parent %s: %w", path, err)
		}

		if root != nil {
			infos[index], err = root.Lstat(filepath.Base(path))
			_ = root.Close()

			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect runner/install file %s: %w", path, err)
			}

			if infos[index] != nil && !infos[index].Mode().IsRegular() {
				return fmt.Errorf("runner/install destination %s must be a nonlinked regular file: %w", path, errs.ErrValidation)
			}
		}

		for previous := range index {
			if path == files[previous] || (infos[index] != nil && infos[previous] != nil && os.SameFile(infos[index], infos[previous])) {
				return fmt.Errorf("runner/install files alias %s and %s: %w", files[previous], path, errs.ErrValidation)
			}
		}
	}

	return nil
}

// ensureBinHome resolves the tool bin home and creates it.
func ensureBinHome(binHome string) (string, error) {
	resolved, err := resolveBinHome(binHome)
	if err != nil {
		return "", err
	}

	root, err := pathsafe.MkdirRoot(resolved, 0o755)
	if err != nil {
		return "", fmt.Errorf("create bin home %s: %w", resolved, err)
	}

	_ = root.Close()

	return resolved, nil
}

func changelogPrepareDir(runID, runnerTemp string) (string, error) {
	runID = defaultRunID(runID)
	if runID == "" || domainversion.SanitizePathToken(runID) != runID {
		return "", fmt.Errorf("install-changelog-renderer: run-id must contain only letters, digits, '.', '_' or '-': %w", errs.ErrUsage)
	}

	temp, err := defaultRunnerTemp(runnerTemp)
	if err != nil {
		return "", err
	}
	// Installed sources live below this tree and must satisfy the publisher's
	// source spelling rules, unlike setup's ENV-only directory values.
	temp, err = resolveMiseLocalPath(temp, true)
	if err != nil {
		return "", err
	}

	return filepath.Join(temp, "reusable-ci-prepare-mise-"+runID), nil
}

// miseDataDirName is MISE_DATA_DIR's leaf under the prepare tree. The runner
// environment and the installation authority must name the same directory.
const miseDataDirName = "data"

// prepareChangelogMiseEnv uses only the already-resolved, preflighted reset path.
func prepareChangelogMiseEnv(prepareDir, binHome string) ([]string, error) {
	if err := os.RemoveAll(prepareDir); err != nil {
		return nil, fmt.Errorf("remove prior prepare mise dir %s: %w", prepareDir, err)
	}

	for _, name := range []string{"cache", "config", miseDataDirName, "state"} {
		if err := os.MkdirAll(filepath.Join(prepareDir, name), 0o755); err != nil { //nolint:gosec // isolated mise prepare tree read by later CI steps.
			return nil, fmt.Errorf("create prepare mise %s dir: %w", name, err)
		}
	}

	return prepareMiseEnv(os.Environ(), binHome, prepareDir), nil
}

// changelogInstallRoot names the only tree a published renderer may come from:
// the MISE_DATA_DIR this run just reset. Resolving it once lets a linked runner
// temp still compare equal to the resolved paths mise reports below it.
func changelogInstallRoot(prepareDir string) (string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(prepareDir, miseDataDirName))
	if err != nil {
		return "", fmt.Errorf("resolve changelog installation root: %w", err)
	}

	if !validMiseSourcePath(root) {
		return "", fmt.Errorf("invalid changelog installation root %q: %w", root, errs.ErrValidation)
	}

	return root, nil
}

// changelogInstalledPath resolves one reported installation path and requires it
// to stay inside root, so a misconfigured or hostile tool manager cannot nominate
// an executable this run did not install.
func changelogInstalledPath(path, root, what string) (string, error) {
	if !validMiseSourcePath(path) {
		return "", fmt.Errorf("invalid changelog %s %q: %w", what, path, errs.ErrValidation)
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve changelog %s: %w", what, err)
	}

	if !validMiseSourcePath(resolved) || !misePathWithin(resolved, root) {
		return "", fmt.Errorf("changelog %s %q is outside this run's mise installation root: %w", what, resolved, errs.ErrValidation)
	}

	return resolved, nil
}

// resolveChangelogBinary locates the installed renderer binary inside the mise
// tool dir. `mise where` output is tool-manager text, not publication authority:
// the reported install dir and the directory the binary was found in must both
// resolve inside this run's isolated installation root. The layout below that
// root stays mise's business. A leaf link written inside the tree still resolves
// outward under the shared publisher's documented source-symlink support, so this
// binds where the selection came from, not the bytes it ultimately names.
func resolveChangelogBinary(ctx context.Context, runner MiseRunner, env []string, tool, bin, installRoot string) (string, error) {
	installDir, err := runner.Run(ctx, env, "--no-config", "where", tool)
	if err != nil {
		return "", fmt.Errorf("resolve %s install dir: %w", bin, err)
	}

	installDir = strings.TrimSpace(installDir)
	if installDir == "" {
		return "", fmt.Errorf("resolve %s install dir: empty output: %w", bin, errs.ErrValidation)
	}

	installDir, err = changelogInstalledPath(installDir, installRoot, "install dir")
	if err != nil {
		return "", err
	}

	candidate, err := findMiseToolBinary(installDir, bin)
	if err != nil {
		return "", err
	}
	// A linked child directory can leave the root between the two probes.
	if _, err = changelogInstalledPath(filepath.Dir(candidate), installRoot, "executable directory"); err != nil {
		return "", err
	}

	return candidate, nil
}

// publishChangelogBinary preflights the selected renderer and PATH together.
// Do not enumerate its directory: sibling executables are not selected tools.
func publishChangelogBinary(candidate, binHome, bin, pathFile string) (string, error) { //nolint:cyclop // source identity and raw/absolute export validation precede publication.
	if bin != "git-cliff" && bin != "git-chglog" { //nolint:goconst // keep the closed publication allowlist independent of test constants.
		return "", fmt.Errorf("unsupported changelog binary %q: %w", bin, errs.ErrValidation)
	}

	if !validMiseSourcePath(candidate) {
		return "", fmt.Errorf("invalid changelog executable path %q: %w", candidate, errs.ErrValidation)
	}

	candidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve changelog executable: %w", err)
	}

	if !validMiseSourcePath(candidate) {
		return "", fmt.Errorf("invalid resolved changelog executable path %q: %w", candidate, errs.ErrValidation)
	}

	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect changelog executable: %w", err)
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("changelog executable must be an executable regular file: %w", errs.ErrValidation)
	}

	if !validMisePathSpelling(binHome) {
		return "", fmt.Errorf("invalid PATH entry %q: %w", binHome, errs.ErrValidation)
	}
	// One absolute bin home binds the published link, PATH entry and version call.
	binHome, err = filepath.Abs(binHome)
	if err != nil {
		return "", fmt.Errorf("resolve changelog bin home: %w", err)
	}

	selected := []miseExecutable{{name: bin, path: candidate, info: info}}
	if err = publishMiseExecutables(selected, []string{binHome}, binHome, pathFile); err != nil {
		return "", err
	}

	return filepath.Join(binHome, bin), nil
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

// defaultRunnerTemp falls back to the OS temp dir when the run context
// carries no scratch directory.
//
// It does NOT consult $RUNNER_TEMP: value arrives from --runner-temp, whose
// sources already include it (via cienv.TempDir()) alongside $CI_TEMP_DIR.
// See defaultReleaseRunnerTemp in app/release for the same reasoning.
func defaultRunnerTemp(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return value, nil
	}

	// OS temp locations may have trusted aliases such as /var -> /private/var.
	// Do not extend this exception to explicit caller-selected destinations.
	temp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve OS temp directory: %w", err)
	}

	return temp, nil
}

func appendEnvFile(path string, entries ...string) error {
	if err := cliio.AppendLines(path, entries...); err != nil {
		return fmt.Errorf("append env entries: %w", err)
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
		"MISE_DATA_DIR":   filepath.Join(prepareDir, miseDataDirName),
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
	if !exactDownloadVersion(version) {
		return fmt.Errorf("install-changelog-renderer: %s version must be exact MAJOR.MINOR.PATCH: %w", name, errs.ErrUsage)
	}

	return nil
}

func findMiseToolBinary(installDir, bin string) (string, error) {
	candidate := filepath.Join(installDir, bin)
	if executableRegularFile(candidate) {
		return candidate, nil
	}

	entries, err := os.ReadDir(installDir)
	if err != nil {
		return "", fmt.Errorf("search %s for %s: %w", installDir, bin, err)
	}

	// ReadDir is name-sorted and treats the root literally. Probe joined paths
	// without IsDir filtering so legitimate linked child directories still work.
	for _, entry := range entries {
		candidate = filepath.Join(installDir, entry.Name(), bin)
		if executableRegularFile(candidate) {
			return candidate, nil
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
