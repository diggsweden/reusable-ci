// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// MiseRunner is the mise subprocess surface needed by ExposeMiseTools.
type MiseRunner interface {
	Run(ctx context.Context, env []string, args ...string) (string, error)
}

// Root selection must reach mise itself, not just our evidence readers.
type rootedMiseRunner struct {
	MiseRunner
	root string
}

func (r rootedMiseRunner) Run(ctx context.Context, env []string, args ...string) (string, error) {
	return r.MiseRunner.Run(ctx, env, append([]string{"--cd", r.root}, args...)...)
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
// The complete selection is preflighted before publishing PATH or creating links.
// Existing same-file destinations are retained; all other occupied names refuse.
// Absolute sources and source symlinks are supported, not content-authenticated.
func ExposeMiseTools(ctx context.Context, runner MiseRunner, out io.Writer, in ExposeMiseToolsInput) error { //nolint:cyclop // evidence and both enumerations precede exposure.
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

	if _, _, err = readMiseConfigs(root); err != nil {
		return err
	}

	if err = validateToolchainEvidence(root, locked); err != nil {
		return err
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}

	runner = rootedMiseRunner{MiseRunner: runner, root: root}

	binHome, err := resolveBinHome(in.BinHome)
	if err != nil {
		return err
	}

	if in.PathFile == "" {
		return fmt.Errorf("path-file is required: %w", errs.ErrUsage)
	}

	binPaths, err := runner.Run(ctx, nil, "bin-paths")
	if err != nil {
		return fmt.Errorf("enumerate mise bin paths: %w: %w", err, errs.ErrDependencyUnavailable)
	}

	cargoDir, err := resolveCargoBinDir(ctx, runner, root, locked)
	if err != nil {
		return err
	}

	dirs := splitMiseLines(binPaths)
	if cargoDir != "" {
		dirs = append(dirs, cargoDir)
	}

	if err = exposeMiseBinPaths(dirs, binHome, in.PathFile); err != nil {
		return err
	}

	printInstalledTools(ctx, runner, out)

	return nil
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

func resolveCargoBinDir(ctx context.Context, runner MiseRunner, root string, locked bool) (string, error) { //nolint:cyclop // optional rustup evidence, locked invocation and returned executable all need validation.
	configs, _, err := readMiseConfigs(root)
	if err != nil {
		return "", err
	}

	if !rustupDeclared(configs) {
		return "", nil
	}

	hasRustToolchain, err := regularFileExists(root, "rust-toolchain.toml")
	if err != nil {
		return "", err
	}

	if !hasRustToolchain {
		return "", nil
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
		return "", fmt.Errorf("resolve rustup cargo path: %w", err)
	}

	cargoPath = strings.TrimSuffix(strings.TrimSuffix(cargoPath, "\n"), "\r")
	if !validMiseSourcePath(cargoPath) {
		return "", fmt.Errorf("resolve rustup cargo path %q: expected one absolute path: %w", cargoPath, errs.ErrValidation)
	}

	info, err := os.Stat(cargoPath)
	if err != nil {
		return "", fmt.Errorf("inspect rustup cargo path: %w: %w", err, errs.ErrValidation)
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("rustup cargo is not an executable regular file: %w", errs.ErrValidation)
	}

	return filepath.Dir(cargoPath), nil
}

type miseExecutable struct {
	name   string
	path   string
	info   os.FileInfo
	exists bool
}

func exposeMiseBinPaths(dirs []string, binHome, pathFile string) error {
	paths, selected, err := planMiseSources(dirs)
	if err != nil {
		return err
	}

	return publishMiseExecutables(selected, paths, binHome, pathFile)
}

// Publication is create-only: no persisted ownership record authorizes replacing
// an old link, even one that resembles a mise link. Symlink creation is an
// atomic, no-clobber leaf operation through the validated destination root.
// Later I/O failures can leave earlier new links; this is not a transaction.
func publishMiseExecutables(selected []miseExecutable, paths []string, binHome, pathFile string) error { //nolint:cyclop,gocognit,gocyclo // complete destination preflight, including a planned PATH parent, precedes publication.
	for _, path := range paths {
		if !validMiseSourcePath(path) {
			return fmt.Errorf("invalid PATH entry %q: %w", path, errs.ErrValidation)
		}
	}

	if pathFile == "" {
		return fmt.Errorf("path-file is required: %w", errs.ErrUsage)
	}

	binHome, err := filepath.Abs(binHome)
	if err != nil {
		return err
	}

	pathFile, err = filepath.Abs(pathFile)
	if err != nil {
		return err
	}

	if binHome == pathFile || strings.HasPrefix(binHome, pathFile+string(filepath.Separator)) {
		return fmt.Errorf("PATH file overlaps bin home: %w", errs.ErrValidation)
	}

	binRoot, err := pathsafe.OpenRoot(binHome)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("open bin home: %w", err)
	}

	defer func() {
		if binRoot != nil {
			_ = binRoot.Close()
		}
	}()

	pathRoot := binRoot
	if filepath.Dir(pathFile) != binHome {
		pathRoot, err = pathsafe.OpenRoot(filepath.Dir(pathFile))
		if err != nil {
			return fmt.Errorf("open PATH parent: %w", err)
		}
		defer func() { _ = pathRoot.Close() }()
	}

	var pathInfo os.FileInfo
	if pathRoot != nil {
		pathInfo, err = pathRoot.Lstat(filepath.Base(pathFile))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect PATH file: %w", err)
		}

		if pathInfo != nil && !pathInfo.Mode().IsRegular() {
			return fmt.Errorf("PATH destination must be a regular file: %w", errs.ErrValidation)
		}
	}

	for index := range selected {
		executable := &selected[index]
		if pathFile == filepath.Join(binHome, executable.name) || (pathInfo != nil && os.SameFile(pathInfo, executable.info)) {
			return fmt.Errorf("PATH file overlaps executable %q: %w", executable.name, errs.ErrValidation)
		}

		if binRoot != nil {
			executable.exists, err = existingMiseDestination(binRoot, *executable)
			if err != nil {
				return err
			}
		}
	}

	if len(paths) == 0 {
		return nil
	}

	if pathRoot == nil {
		// PATH directly inside a new bin home shares its planned creation,
		// but only after the complete source/destination preflight above.
		binRoot, err = pathsafe.MkdirRoot(binHome, 0o755)
		if err != nil {
			return fmt.Errorf("create bin home for PATH: %w", err)
		}

		pathRoot = binRoot
	}

	flags := os.O_APPEND | os.O_WRONLY
	if pathInfo == nil {
		flags |= os.O_CREATE | os.O_EXCL
	}

	pathOutput, err := pathRoot.OpenFile(filepath.Base(pathFile), flags, 0o644)
	if err != nil {
		return fmt.Errorf("open PATH file: %w", err)
	}

	defer func() { _ = pathOutput.Close() }()

	if binRoot == nil {
		binRoot, err = pathsafe.MkdirRoot(binHome, 0o755)
		if err != nil {
			return fmt.Errorf("create bin home: %w", err)
		}
	}

	for _, executable := range selected {
		if executable.exists {
			continue
		}

		if err = binRoot.Symlink(executable.path, executable.name); err != nil {
			return fmt.Errorf("create executable link %q: %w", executable.name, err)
		}
	}

	_, writeErr := io.WriteString(pathOutput, strings.Join(paths, "\n")+"\n")
	if err = errors.Join(writeErr, pathOutput.Close()); err != nil {
		return fmt.Errorf("append exposed paths: %w", err)
	}

	return nil
}

func planMiseSources(dirs []string) ([]string, []miseExecutable, error) {
	var (
		selected []miseExecutable
		paths    []string
	)

	seenDirs := make(map[string]bool)
	seenNames := make(map[string]os.FileInfo)

	for _, dir := range dirs {
		canonical, executables, err := inspectMiseBinDir(dir)
		if err != nil {
			return nil, nil, err
		}

		if canonical == "" || seenDirs[canonical] {
			continue
		}

		seenDirs[canonical] = true
		paths = append(paths, canonical)

		for _, executable := range executables {
			if previous, ok := seenNames[executable.name]; ok {
				if !os.SameFile(previous, executable.info) {
					return nil, nil, fmt.Errorf("conflicting executable basename %q: %w", executable.name, errs.ErrValidation)
				}

				continue
			}

			seenNames[executable.name] = executable.info
			selected = append(selected, executable)
		}
	}

	return paths, selected, nil
}

func validMiseSourcePath(path string) bool {
	return filepath.IsAbs(path) && validMisePathSpelling(path)
}

func validMisePathSpelling(path string) bool {
	return !strings.ContainsAny(path, "\x00\r\n\t"+string(os.PathListSeparator))
}

func inspectMiseBinDir(dir string) (string, []miseExecutable, error) { //nolint:cyclop // source links are resolved, while missing directories alone retain the documented skip policy.
	if !validMiseSourcePath(dir) {
		return "", nil, fmt.Errorf("invalid mise bin directory %q: %w", dir, errs.ErrValidation)
	}

	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	} else if err != nil {
		return "", nil, fmt.Errorf("inspect mise bin directory: %w", err)
	}

	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve mise bin directory: %w", err)
	}

	if !validMiseSourcePath(dir) {
		return "", nil, fmt.Errorf("invalid resolved mise bin directory %q: %w", dir, errs.ErrValidation)
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return "", nil, err
	}

	defer func() { _ = root.Close() }()

	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return "", nil, fmt.Errorf("read mise bin directory: %w", err)
	}

	var executables []miseExecutable

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if !validMiseSourcePath(path) {
			return "", nil, fmt.Errorf("invalid executable name %q: %w", entry.Name(), errs.ErrValidation)
		}
		// Resolve legitimate relative/absolute rustup links, including targets
		// outside this bin directory. Do not turn source links into self-links.
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", nil, fmt.Errorf("resolve executable %q: %w", entry.Name(), err)
		}

		if !validMiseSourcePath(path) {
			return "", nil, fmt.Errorf("invalid resolved executable path %q: %w", path, errs.ErrValidation)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", nil, fmt.Errorf("inspect executable %q: %w", entry.Name(), statErr)
		}

		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("executable entry %q is not a regular file: %w", entry.Name(), errs.ErrValidation)
		}

		if info.Mode().Perm()&0o111 == 0 {
			continue
		}

		executables = append(executables, miseExecutable{name: entry.Name(), path: path, info: info})
	}

	return dir, executables, nil
}

func existingMiseDestination(root *os.Root, executable miseExecutable) (bool, error) {
	info, err := root.Lstat(executable.name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("inspect executable destination %q: %w", executable.name, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// Resolve the destination itself: cleaning its link target would
		// change symlink/.. traversal. Valid targets may be outside bin home.
		info, err = os.Stat(filepath.Join(root.Name(), executable.name))
	}

	if err == nil && os.SameFile(info, executable.info) {
		return true, nil
	}

	return false, fmt.Errorf("executable destination %q is occupied by an unrelated entry: %w", executable.name, errs.ErrValidation)
}

func appendPathFile(pathFile, dir string) error {
	if err := cliio.AppendLines(pathFile, dir); err != nil {
		return fmt.Errorf("append %s to path file: %w", dir, err)
	}

	return nil
}

func splitMiseLines(value string) []string {
	var out []string

	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSuffix(line, "\r")
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
