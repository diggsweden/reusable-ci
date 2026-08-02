// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	defaultBuildahRunnerTemp = "/tmp"
	probeImage               = "localhost/forgejo-ci-storage-probe:latest"

	// buildahBinary and fuseOverlayfsBinary are the command/package names of
	// the Buildah runtime dependencies this setup installs and probes.
	buildahBinary       = "buildah"
	fuseOverlayfsBinary = "fuse-overlayfs"
)

var aptPackageRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

// BuildahSetupTool is the Buildah/system surface needed by SetupBuildah.
type BuildahSetupTool interface {
	CommandExists(name string) bool
	InfoDriver(ctx context.Context, env []string) (string, error)
	InfoJSON(ctx context.Context, env []string) ([]byte, error)
	ProbeBuild(ctx context.Context, env []string, probeDir, image string, out io.Writer) error
	RemoveImage(ctx context.Context, env []string, image string, out io.Writer) error
}

// PackageInstaller installs Debian packages selected by SetupBuildah.
type PackageInstaller interface {
	Install(ctx context.Context, packages []string, out io.Writer) error
}

// SetupBuildahInput drives `container setup-buildah`.
type SetupBuildahInput struct {
	ExtraPackages   string
	InstallPackages bool
	ProbeBuild      bool
	PrintStore      bool
	WriteSummary    bool
	StorageConf     string
	StorageRoot     string
	TmpDir          string
	RunnerTemp      string
	EnvFile         string
}

// SetupBuildahResult is the selected storage configuration.
type SetupBuildahResult struct {
	Driver      string
	StorageConf string
	StorageRoot string
	TmpDir      string
}

// SetupBuildah installs missing Buildah runtime packages when requested and
// configures job-local container storage. It prefers overlay/fuse-overlayfs only
// after buildah info and an optional scratch-image probe succeed, then falls back
// to vfs. The selected storage paths are emitted for later CI steps through the
// runner env file and the OutputSink.
func SetupBuildah(
	ctx context.Context,
	tool BuildahSetupTool,
	installer PackageInstaller,
	sink ci.OutputSink,
	summary ci.SummarySink,
	out io.Writer,
	in SetupBuildahInput,
) (*SetupBuildahResult, error) {
	if tool == nil {
		return nil, fmt.Errorf("buildah setup tool is required: %w", errs.ErrUsage)
	}

	packages, err := setupBuildahPackages(in.ExtraPackages)
	if err != nil {
		return nil, err
	}

	if in.InstallPackages {
		if err = installBuildahPackages(ctx, tool, installer, packages, out); err != nil {
			return nil, err
		}
	}

	if !tool.CommandExists(buildahBinary) {
		return nil, fmt.Errorf("buildah is not installed: %w", errs.ErrDependencyUnavailable)
	}

	paths, err := setupBuildahPaths(in)
	if err != nil {
		return nil, err
	}

	driver, err := configureBuildahStorage(ctx, tool, out, paths, in.ProbeBuild)
	if err != nil {
		return nil, err
	}

	printFilesystemDiagnostics(out, []string{"/tmp", "/var/tmp", paths.StorageRoot, paths.TmpDir})

	result := &SetupBuildahResult{
		Driver:      driver,
		StorageConf: paths.StorageConf,
		StorageRoot: paths.StorageRoot,
		TmpDir:      paths.TmpDir,
	}

	if err := emitBuildahSetup(ctx, sink, summary, out, result, in.EnvFile, in.WriteSummary); err != nil {
		return nil, err
	}

	if in.PrintStore {
		printBuildahStore(ctx, tool, out, paths)
	}

	return result, nil
}

type buildahSetupPaths struct {
	StorageConf string
	StorageRoot string
	TmpDir      string
	ProbeDir    string
}

func setupBuildahPackages(extra string) ([]string, error) {
	packages := []string{buildahBinary, fuseOverlayfsBinary}
	seen := map[string]bool{buildahBinary: true, fuseOverlayfsBinary: true}

	for _, packageName := range strings.Fields(strings.ReplaceAll(extra, "\n", " ")) {
		if !aptPackageRE.MatchString(packageName) {
			return nil, fmt.Errorf("unsafe apt package name: %s: %w", packageName, errs.ErrUsage)
		}

		if !seen[packageName] {
			packages = append(packages, packageName)
			seen[packageName] = true
		}
	}

	return packages, nil
}

func installBuildahPackages(ctx context.Context, tool BuildahSetupTool, installer PackageInstaller, packages []string, out io.Writer) error {
	if !tool.CommandExists("apt-get") {
		return fmt.Errorf("apt-get is required when install-packages is true: %w", errs.ErrDependencyUnavailable)
	}

	if installer == nil {
		return fmt.Errorf("package installer is required when install-packages is true: %w", errs.ErrUsage)
	}

	missing := missingBuildahPackages(tool, packages)
	if len(missing) == 0 {
		return nil
	}

	return installer.Install(ctx, missing, out)
}

func missingBuildahPackages(tool BuildahSetupTool, packages []string) []string {
	var missing []string

	for _, packageName := range packages {
		commandName := buildahPackageCommand(packageName)
		if commandName == "" || !tool.CommandExists(commandName) {
			missing = append(missing, packageName)
		}
	}

	return missing
}

func buildahPackageCommand(packageName string) string {
	switch packageName {
	case buildahBinary, fuseOverlayfsBinary, "curl", "jq", "podman", "skopeo", "unzip":
		return packageName
	case "gettext":
		return "envsubst"
	default:
		return ""
	}
}

func setupBuildahPaths(in SetupBuildahInput) (buildahSetupPaths, error) {
	runnerTemp := defaultString(in.RunnerTemp, defaultBuildahRunnerTemp)
	paths := buildahSetupPaths{
		StorageConf: defaultString(in.StorageConf, filepath.Join(runnerTemp, "containers-storage.conf")),
		StorageRoot: defaultString(in.StorageRoot, filepath.Join(runnerTemp, "containers-storage")),
		TmpDir:      defaultString(in.TmpDir, filepath.Join(runnerTemp, "container-tmp")),
		ProbeDir:    filepath.Join(runnerTemp, "container-storage-probe"),
	}

	if err := rejectUnsafeBuildahPath("container storage root", paths.StorageRoot, true); err != nil {
		return buildahSetupPaths{}, err
	}

	if err := rejectUnsafeBuildahPath("containers storage config path", paths.StorageConf, false); err != nil {
		return buildahSetupPaths{}, err
	}

	if err := rejectUnsafeBuildahPath("container temp directory", paths.TmpDir, true); err != nil {
		return buildahSetupPaths{}, err
	}

	return paths, nil
}

func rejectUnsafeBuildahPath(label, path string, directory bool) error {
	clean := filepath.Clean(path)
	if path == "" || clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("unsafe %s: %s: %w", label, path, errs.ErrUsage)
	}

	if directory {
		switch clean {
		case "/tmp", "/var", "/var/lib", "/var/tmp", "/run":
			return fmt.Errorf("unsafe %s: %s: %w", label, path, errs.ErrUsage)
		}
	}

	return nil
}

func configureBuildahStorage(ctx context.Context, tool BuildahSetupTool, out io.Writer, paths buildahSetupPaths, probe bool) (string, error) {
	if tool.CommandExists(fuseOverlayfsBinary) {
		if err := writeBuildahStorage(ctx, tool, out, paths, "overlay", probe); err == nil {
			return "overlay", nil
		} else if out != nil {
			_, _ = fmt.Fprintf(out, "overlay/fuse-overlayfs unavailable; falling back to vfs: %v\n", err)
		}
	} else if out != nil {
		_, _ = fmt.Fprintln(out, "fuse-overlayfs missing; falling back to vfs")
	}

	if err := writeBuildahStorage(ctx, tool, out, paths, "vfs", probe); err != nil {
		return "", err
	}

	return "vfs", nil
}

func writeBuildahStorage(ctx context.Context, tool BuildahSetupTool, out io.Writer, paths buildahSetupPaths, driver string, probe bool) error {
	if err := resetBuildahStorage(paths); err != nil {
		return err
	}

	if err := writeBuildahStorageConf(paths, driver); err != nil {
		return err
	}

	env := buildahStorageEnv(paths)

	actualDriver, err := tool.InfoDriver(ctx, env)
	if err != nil {
		return err
	}

	if actualDriver != driver {
		return fmt.Errorf("buildah selected %s storage after %s config: %w", actualDriver, driver, errs.ErrValidation)
	}

	if probe {
		return runBuildahProbe(ctx, tool, env, paths.ProbeDir, out)
	}

	return nil
}

func resetBuildahStorage(paths buildahSetupPaths) error {
	if err := os.RemoveAll(paths.StorageRoot); err != nil {
		return fmt.Errorf("reset container storage root: %w", err)
	}

	if err := os.RemoveAll(paths.ProbeDir); err != nil {
		return fmt.Errorf("reset container storage probe dir: %w", err)
	}

	for _, dir := range []string{paths.StorageRoot, paths.ProbeDir, paths.TmpDir, filepath.Dir(paths.StorageConf)} {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec,mnd // job-local runner-temp storage dirs; buildah needs them traversable.
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return nil
}

func writeBuildahStorageConf(paths buildahSetupPaths, driver string) error {
	var body string

	switch driver {
	case "overlay":
		body = fmt.Sprintf("[storage]\ndriver = \"overlay\"\nrunroot = \"%s/run\"\ngraphroot = \"%s/graph\"\n\n[storage.options.overlay]\nmount_program = \"/usr/bin/fuse-overlayfs\"\n", paths.StorageRoot, paths.StorageRoot)
	case "vfs":
		body = fmt.Sprintf("[storage]\ndriver = \"vfs\"\nrunroot = \"%s/run-vfs\"\ngraphroot = \"%s/graph-vfs\"\n", paths.StorageRoot, paths.StorageRoot)
	default:
		return fmt.Errorf("unsupported storage driver: %s: %w", driver, errs.ErrUsage)
	}

	return cliio.WriteFile(paths.StorageConf, []byte(body), 0o644)
}

func runBuildahProbe(ctx context.Context, tool BuildahSetupTool, env []string, probeDir string, out io.Writer) error {
	if err := cliio.WriteFile(filepath.Join(probeDir, "probe.txt"), []byte("probe\n"), 0o644); err != nil {
		return err
	}

	if err := cliio.WriteFile(filepath.Join(probeDir, "Containerfile"), []byte("FROM scratch\nCOPY probe.txt /probe.txt\n"), 0o644); err != nil {
		return err
	}

	_ = tool.RemoveImage(context.WithoutCancel(ctx), env, probeImage, io.Discard)

	return tool.ProbeBuild(ctx, env, probeDir, probeImage, out)
}

func buildahStorageEnv(paths buildahSetupPaths) []string {
	return []string{
		"CONTAINERS_STORAGE_CONF=" + paths.StorageConf,
		"TMPDIR=" + paths.TmpDir,
	}
}

func emitBuildahSetup(ctx context.Context, sink ci.OutputSink, summary ci.SummarySink, out io.Writer, result *SetupBuildahResult, envFile string, writeSummary bool) error {
	if envFile != "" {
		if err := appendBuildahEnvFile(envFile, result); err != nil {
			return err
		}
	}

	if err := emitBuildahSetupOutputs(ctx, sink, result); err != nil {
		return err
	}

	if out != nil {
		_, _ = fmt.Fprintf(out, "Container storage driver: %s\n", result.Driver)
		_, _ = fmt.Fprintf(out, "Container storage config: %s\n", result.StorageConf)
		_, _ = fmt.Fprintf(out, "Container storage root: %s\n", result.StorageRoot)
		_, _ = fmt.Fprintf(out, "Container temp dir: %s\n", result.TmpDir)
	}

	if writeSummary && summary != nil {
		body := fmt.Sprintf("### Container storage\n\n* Driver: `%s`\n* Config: `%s`\n* Graph root: `%s`\n", result.Driver, result.StorageConf, result.StorageRoot)
		if err := summary.Append(ctx, body); err != nil {
			return err
		}
	}

	return nil
}

// emitBuildahSetupOutputs publishes the selected storage configuration to the
// CI output sink.
func emitBuildahSetupOutputs(ctx context.Context, sink ci.OutputSink, result *SetupBuildahResult) error {
	if sink == nil {
		return nil
	}

	if err := sink.Set(ctx, "driver", result.Driver); err != nil {
		return err
	}

	if err := sink.Set(ctx, "storage-conf", result.StorageConf); err != nil {
		return err
	}

	return sink.Set(ctx, "storage-root", result.StorageRoot)
}

func appendBuildahEnvFile(path string, result *SetupBuildahResult) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // CI runner env file chosen by caller.
	if err != nil {
		return fmt.Errorf("open env file %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	if _, err := fmt.Fprintf(file, "CONTAINERS_STORAGE_CONF=%s\nTMPDIR=%s\n", result.StorageConf, result.TmpDir); err != nil {
		return fmt.Errorf("append buildah env file %s: %w", path, err)
	}

	return nil
}

func printBuildahStore(ctx context.Context, tool BuildahSetupTool, out io.Writer, paths buildahSetupPaths) {
	if out == nil {
		return
	}

	body, err := tool.InfoJSON(ctx, buildahStorageEnv(paths))
	if err != nil {
		_, _ = fmt.Fprintf(out, "buildah info unavailable: %v\n", err)

		return
	}

	var info map[string]any
	if err := json.Unmarshal(body, &info); err == nil {
		if store, ok := info["store"]; ok {
			if pretty, marshalErr := json.MarshalIndent(store, "", "  "); marshalErr == nil {
				_, _ = fmt.Fprintln(out, string(pretty))

				return
			}
		}
	}

	_, _ = out.Write(body)
}

func printFilesystemDiagnostics(out io.Writer, paths []string) {
	if out == nil {
		return
	}

	seen := map[string]bool{}
	_, _ = fmt.Fprintln(out, "Filesystem capacity diagnostics:")

	for _, path := range paths {
		if seen[path] {
			continue
		}

		seen[path] = true
		printFilesystemStat(out, path, false)
	}

	seen = map[string]bool{}
	_, _ = fmt.Fprintln(out, "Filesystem inode diagnostics:")

	for _, path := range paths {
		if seen[path] {
			continue
		}

		seen[path] = true
		printFilesystemStat(out, path, true)
	}
}

func printFilesystemStat(out io.Writer, path string, inodes bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		_, _ = fmt.Fprintf(out, "  %s: unavailable (%v)\n", path, err)

		return
	}

	if inodes {
		_, _ = fmt.Fprintf(out, "  %s: free=%d total=%d\n", path, stat.Ffree, stat.Files)

		return
	}

	_, _ = fmt.Fprintf(out, "  %s: avail=%d total=%d\n", path, stat.Bavail*uint64(stat.Bsize), stat.Blocks*uint64(stat.Bsize)) //nolint:gosec // Bsize is a positive filesystem block size; conversion cannot overflow.
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}
