// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// CargoTool runs `cargo` commands. The shape mirrors GoTool: a single
// Run method takes a struct of (dir, env, args, stdout, stderr) so
// adapters and fakes share one interface. CargoRunInput is a type alias
// of GoRunInput — the field set is generic exec input.
//
//nolint:iface // intentionally distinct consumer-defined port — see GoTool.
type CargoTool interface {
	Run(ctx context.Context, in CargoRunInput) error
}

// CargoRunInput aliases GoRunInput. Same shape — separate name keeps
// call-sites readable when both tools appear nearby.
type CargoRunInput = domainbuild.GoRunInput

// CargoMetadataInput drives CargoMetadata.
type CargoMetadataInput struct {
	Dir          string
	ArtifactName string
	BinaryName   string
	Version      string
	RefName      string
}

// CargoTestInput drives CargoTest.
type CargoTestInput struct {
	Dir string
}

// CargoBuildBinariesInput drives CargoBuildBinaries.
type CargoBuildBinariesInput struct {
	Dir string
	// BinaryName is the basename the binary is renamed to under dist/.
	// Empty → CrateBinaryName.
	BinaryName string
	// CrateBinaryName is the name cargo itself gives the binary (the [[bin]]
	// target, else the package name) and so the file it writes under
	// target/<triple>/release/. Empty → read from `cargo metadata`.
	CrateBinaryName string
	Platforms       string
	Version         string
	RefName         string
	Commit          string
}

// CargoFetch runs `cargo fetch --locked`. --locked enforces Cargo.lock,
// so a dependency that drifted vs the lockfile fails the build at fetch
// time rather than producing a non-reproducible binary at compile time.
func CargoFetch(ctx context.Context, tool CargoTool, w, stderr io.Writer, dir string) error {
	return tool.Run(ctx, CargoRunInput{
		Dir:    defaultCargoDir(dir),
		Args:   []string{"fetch", flagCargoLocked},
		Stdout: w,
		Stderr: stderr,
	})
}

// CargoTest runs `cargo test --locked` against the workspace. Tests
// remain caller-owned in pull-request quality flows; this is the release
// build's pre-compile gate, opt-out via --skip-tests.
func CargoTest(ctx context.Context, tool CargoTool, w, stderr io.Writer, in CargoTestInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	return tool.Run(ctx, CargoRunInput{
		Dir:    defaultCargoDir(in.Dir),
		Args:   []string{"test", flagCargoLocked, "--all-targets"},
		Stdout: w,
		Stderr: stderr,
	})
}

// CargoBuildBinaries cross-compiles one release binary per requested
// platform into dist/<goos>-<goarch>/<binary>-<goos>-<goarch>. The output
// layout matches GoBuildBinaries so downstream SBOM / release-packaging
// code does not have to special-case the ecosystem.
//
// Whether a target actually links depends on the runtime image carrying
// `rustup target add <triple>` and the matching cross-linker. We don't
// model that here — `cargo build` produces a precise error when a target
// is missing, which surfaces in the workflow log.
func CargoBuildBinaries(ctx context.Context, tool CargoTool, w, stderr io.Writer, in CargoBuildBinariesInput) error { //nolint:cyclop,gocognit,varnamelen // matrix loop keeps each target's source cleanup, build and checked copy together.
	dir := defaultCargoDir(in.Dir)

	binaryName := strings.TrimSpace(in.BinaryName)
	crateBinary := strings.TrimSpace(in.CrateBinaryName)

	if binaryName == "" || crateBinary == "" {
		meta, err := readCargoMetadata(ctx, tool, dir)
		if err != nil {
			return err
		}

		if crateBinary == "" {
			crateBinary = meta.crateBinaryName
		}

		if binaryName == "" {
			binaryName = crateBinary
		}
	}

	// Reject scalar shapes that would break the GHA output contract or
	// allow injection of fake ldflags-style lines (mirrors Go path).
	if err := validateScalarValue(binaryName); err != nil {
		return fmt.Errorf("binary-name: %w", err)
	}

	for _, name := range []string{binaryName, crateBinary} {
		if !pathsafe.Relative(name) || name == "." || filepath.Base(name) != name {
			return fmt.Errorf("cargo binary names must be plain basenames: %w", errs.ErrUsage)
		}
	}

	version := domainversion.StripVPrefix(strings.TrimSpace(in.Version))
	if version == "" {
		version = domainversion.StripVPrefix(strings.TrimSpace(in.RefName))
	}

	if version == "" {
		return fmt.Errorf("version is required (pass --version or --ref-name, or set $VERSION or $REF_NAME; use 'dev' for local builds): %w", errs.ErrUsage)
	}

	if err := validateScalarValue(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}

	platforms, err := splitCargoPlatforms(in.Platforms)
	if err != nil {
		return err
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	dir = root.Name()

	// Targeted dist/<goos>-<goarch> wipe — never blanket-RemoveAll(dist)
	// since callers may stage sibling assets there. Same rationale as
	// GoBuildBinaries.
	for _, platform := range platforms {
		goos, goarch, _ := parseCargoPlatform(platform)
		if rmErr := root.RemoveAll(filepath.Join("dist", goos+"-"+goarch)); rmErr != nil {
			return fmt.Errorf("remove dist/%s-%s: %w", goos, goarch, rmErr)
		}
	}

	for _, platform := range platforms {
		goos, goarch, _ := parseCargoPlatform(platform)
		triple := domainbuild.CargoTargetTriple(platform)

		outDir := filepath.Join(dir, "dist", goos+"-"+goarch)
		if err := root.MkdirAll(filepath.Join("dist", goos+"-"+goarch), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", outDir, err)
		}

		outName := fmt.Sprintf("%s-%s-%s", binaryName, goos, goarch)
		if goos == archGOOSWindows {
			outName += extExe
		}

		_, _ = fmt.Fprintf(w, "Building %s -> %s\n", triple, filepath.Join(outDir, outName))

		sourceName := crateBinary
		if goos == archGOOSWindows {
			sourceName += extExe
		}

		source := filepath.Join(dir, "target", triple, "release", sourceName)

		parent, parentErr := pathsafe.OpenRoot(filepath.Dir(source))
		if parentErr != nil && !errors.Is(parentErr, os.ErrNotExist) {
			return parentErr
		}

		if parent != nil {
			removeErr := parent.Remove(sourceName)
			_ = parent.Close()

			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("remove previous Cargo binary: %w", removeErr)
			}
		}

		args := []string{
			subCmdBuild, "--release", flagCargoLocked,
			"--target", triple,
			"--bin", crateBinary,
			"--target-dir", filepath.Join(dir, "target"),
		}
		if err := tool.Run(ctx, CargoRunInput{
			Dir:    dir,
			Env:    []string{"REUSABLE_CI_VERSION=" + version, "REUSABLE_CI_COMMIT=" + strings.TrimSpace(in.Commit)},
			Args:   args,
			Stdout: w,
			Stderr: stderr,
		}); err != nil {
			return err
		}

		// Cargo writes target/<triple>/release/<crate_name>(.exe). Locate
		// it and copy to the dist/ layout. The crate name (which may differ
		// from the --binary-name override) names the source; the
		// destination uses binaryName per the GHA output contract.
		built, err := locateCargoBinary(dir, triple, goos, crateBinary)
		if err != nil {
			return err
		}

		if err := copyFile(built, filepath.Join(outDir, outName)); err != nil {
			return err
		}
	}

	return nil
}

// CargoMetadata reads Cargo.toml metadata (via `cargo metadata`) and
// emits the build contract: binary-name, version, package. Mirrors
// GoMetadata's output keys so downstream workflow steps don't have to
// branch on ecosystem.
func CargoMetadata(ctx context.Context, tool CargoTool, sink ci.OutputSink, w io.Writer, in CargoMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	meta, err := resolveCargoMetadata(ctx, tool, in)
	if err != nil {
		return err
	}

	outputs := []struct{ key, value string }{
		{outKeyBinaryName, meta.binaryName},
		{outKeyVersion, meta.version},
		{outKeyPackage, meta.packageName},
	}
	for _, out := range outputs {
		if err := sink.Set(ctx, out.key, out.value); err != nil {
			if errors.Is(err, errs.ErrInvalidConfig) || errors.Is(err, errs.ErrValidation) {
				return err
			}

			return fmt.Errorf("emit output %q: %w", out.key, err)
		}
	}

	_, _ = fmt.Fprintf(w, "Binary: %s\nPackage: %s\nVersion: %s\n", meta.binaryName, meta.packageName, meta.version)

	return nil
}

// resolveCargoMetadata reads Cargo.toml/cargo metadata and applies the
// binary-name + version precedence. Shared by CargoMetadata (emits) and
// CargoReleaseBuild (threads the values through the build). The returned
// cargoMetadata carries the *resolved* binary-name/version.
//
//nolint:cyclop // input-validation flow mirrors resolveGoMetadata.
func resolveCargoMetadata(ctx context.Context, tool CargoTool, in CargoMetadataInput) (cargoMetadata, error) {
	meta, err := readCargoMetadata(ctx, tool, defaultCargoDir(in.Dir))
	if err != nil {
		return cargoMetadata{}, err
	}

	binaryName := firstNonEmpty(in.BinaryName, in.ArtifactName, meta.binaryName)
	if err := validateScalarValue(binaryName); err != nil {
		return cargoMetadata{}, fmt.Errorf("binary-name: %w", err)
	}

	meta.binaryName = binaryName

	version := domainversion.StripVPrefix(strings.TrimSpace(in.Version))
	if version == "" {
		version = domainversion.StripVPrefix(strings.TrimSpace(in.RefName))
	}

	if version != "" {
		if err := validateScalarValue(version); err != nil {
			return cargoMetadata{}, fmt.Errorf("version: %w", err)
		}
	}

	if version == "" {
		version = meta.version
	}

	if version == "" {
		version = "dev"
	}

	meta.version = version

	return meta, nil
}

// cargoMetadata is the subset of `cargo metadata --format-version 1`
// output we parse — enough to derive binary-name, version, package.
type cargoMetadata struct {
	// binaryName is the resolved output name (override → artifact name →
	// crate binary); crateBinaryName is always what cargo itself writes.
	binaryName      string
	crateBinaryName string
	version         string
	packageName     string
}

// cargoPackage is one entry of `cargo metadata`'s packages array.
type cargoPackage struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	ManifestPath string `json:"manifest_path"` //nolint:tagliatelle // schema field is exactly this.
	Targets      []struct {
		Name string   `json:"name"`
		Kind []string `json:"kind"`
	} `json:"targets"`
}

// readCargoMetadata shells out to `cargo metadata --no-deps
// --format-version 1` and picks the root package. For workspaces with
// no root [package], the first workspace member is used — same heuristic
// as cargo-cyclonedx and `cargo run` defaults.
func readCargoMetadata(ctx context.Context, tool CargoTool, dir string) (cargoMetadata, error) {
	var buf strings.Builder

	if err := tool.Run(ctx, CargoRunInput{
		Dir:    dir,
		Args:   []string{"metadata", "--no-deps", "--format-version", "1"},
		Stdout: &buf,
		Stderr: io.Discard,
	}); err != nil {
		return cargoMetadata{}, fmt.Errorf("cargo metadata: %w", err)
	}

	var doc struct {
		Packages      []cargoPackage `json:"packages"`
		WorkspaceRoot string         `json:"workspace_root"`    //nolint:tagliatelle // schema field is exactly this.
		WorkspaceMems []string       `json:"workspace_members"` //nolint:tagliatelle // schema field is exactly this.
		Resolve       any            `json:"resolve"`
	}

	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&doc); err != nil {
		return cargoMetadata{}, fmt.Errorf("parse cargo metadata: %w", err)
	}

	if len(doc.Packages) == 0 {
		return cargoMetadata{}, fmt.Errorf("cargo metadata returned no packages: %w", errs.ErrInvalidConfig)
	}

	// Pick the root package. cargo sorts `packages` by package id, so the
	// [package] at workspace_root is not necessarily first (cargo 1.98 lists
	// a member "alpha" before the root "zeta"); it is the one whose manifest
	// is <workspace_root>/Cargo.toml. A virtual workspace has no such
	// package and keeps the first member — the cargo-cyclonedx / `cargo run`
	// heuristic. Single-crate projects have exactly one entry.
	root := doc.Packages[0]
	if rootPkg, ok := workspaceRootPackage(doc.Packages, doc.WorkspaceRoot); ok {
		root = rootPkg
	}

	// Output renaming is not source selection. Refuse ambiguous metadata
	// rather than silently labeling the last binary as the release.
	binaryName := root.Name
	binCount := 0

	for _, t := range root.Targets {
		for _, k := range t.Kind {
			if k == "bin" && t.Name != "" {
				binaryName = t.Name
				binCount++

				break
			}
		}
	}

	if binCount > 1 {
		return cargoMetadata{}, fmt.Errorf("multiple Cargo binaries require an explicit crate binary selection: %w", errs.ErrUsage)
	}

	return cargoMetadata{
		binaryName:      binaryName,
		crateBinaryName: binaryName,
		version:         root.Version,
		packageName:     root.Name,
	}, nil
}

// workspaceRootPackage returns the package whose manifest is the workspace
// root's Cargo.toml, if any.
func workspaceRootPackage(packages []cargoPackage, workspaceRoot string) (cargoPackage, bool) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return cargoPackage{}, false
	}

	want := filepath.Join(filepath.Clean(workspaceRoot), "Cargo.toml")

	for _, candidate := range packages {
		if candidate.ManifestPath != "" && filepath.Clean(candidate.ManifestPath) == want {
			return candidate, true
		}
	}

	return cargoPackage{}, false
}

// locateCargoBinary returns the path to the produced binary inside
// target/<triple>/release/. cargo names the binary after the [[bin]]
// target (or, by default, the package name). The override flow uses
// --binary-name to rename in dist/ but cargo still writes the source
// under its own naming. An unrelated basename is never accepted as a fallback.
func locateCargoBinary(dir, triple, goos, name string) (string, error) {
	suffix := ""
	if goos == archGOOSWindows {
		suffix = extExe
	}

	candidate := filepath.Join(dir, "target", triple, "release", name+suffix)
	if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() && info.Size() > 0 && (goos == archGOOSWindows || info.Mode().Perm()&0o111 != 0) {
		return candidate, nil
	}

	return "", fmt.Errorf("compiled binary not found under target/%s/release/ (looked for %q): %w", triple, name+suffix, errs.ErrInvalidConfig)
}

func copyFile(src, dst string) error {
	root, err := pathsafe.OpenRoot(filepath.Dir(src))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	in, err := root.Open(filepath.Base(src))
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("cargo output must be a nonempty regular file: %w", errs.ErrValidation)
	}

	stage, err := pathsafe.NewArtifactStaging(filepath.Dir(dst))
	if err != nil {
		return err
	}

	defer func() { _ = stage.Close() }()

	out, err := stage.Root().OpenFile(filepath.Base(dst), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}

	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}

	if err := out.Close(); err != nil {
		return err
	}

	return stage.Install()
}

func defaultCargoDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "."
	}

	return dir
}

func splitCargoPlatforms(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{domainbuild.DefaultPlatform}, nil
	}

	platforms := make([]string, 0)

	for _, raw := range listval.Tokens(value) {
		platform := strings.TrimSpace(raw)
		if platform == "" {
			continue
		}

		if _, _, err := parseCargoPlatform(platform); err != nil {
			return nil, err
		}

		platforms = append(platforms, platform)
	}

	if len(platforms) == 0 {
		return nil, fmt.Errorf("platforms is empty: %w", errs.ErrUsage)
	}

	return platforms, nil
}

func parseCargoPlatform(platform string) (string, string, error) {
	goos, goarch, err := domainbuild.SplitPlatform(platform)
	if err != nil {
		return "", "", err
	}

	if !domainbuild.IsKnownCargoPlatform(goos + "/" + goarch) {
		return "", "", fmt.Errorf("unsupported platform %q for cargo (no Rust target triple mapping): %w", platform, errs.ErrUsage)
	}

	return goos, goarch, nil
}
