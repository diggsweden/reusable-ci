// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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

	domainbuild "github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/internal/domain/version"
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
	Dir        string
	BinaryName string
	Platforms  string
	Version    string
	RefName    string
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
func CargoBuildBinaries(ctx context.Context, tool CargoTool, w, stderr io.Writer, in CargoBuildBinariesInput) error { //nolint:cyclop,varnamelen // matrix loop mirrors GoBuildBinaries.
	dir := defaultCargoDir(in.Dir)

	binaryName := strings.TrimSpace(in.BinaryName)
	if binaryName == "" {
		meta, err := readCargoMetadata(ctx, tool, dir)
		if err != nil {
			return err
		}

		binaryName = meta.binaryName
	}

	// Reject scalar shapes that would break the GHA output contract or
	// allow injection of fake ldflags-style lines (mirrors Go path).
	if err := validateScalarValue(binaryName); err != nil {
		return fmt.Errorf("binary-name: %w", err)
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

	// Targeted dist/<goos>-<goarch> wipe — never blanket-RemoveAll(dist)
	// since callers may stage sibling assets there. Same rationale as
	// GoBuildBinaries.
	for _, platform := range platforms {
		goos, goarch, _ := parseCargoPlatform(platform)
		if rmErr := os.RemoveAll(filepath.Join(dir, "dist", goos+"-"+goarch)); rmErr != nil {
			return fmt.Errorf("remove dist/%s-%s: %w", goos, goarch, rmErr)
		}
	}

	for _, platform := range platforms {
		goos, goarch, _ := parseCargoPlatform(platform)
		triple := domainbuild.CargoTargetTriple(platform)

		outDir := filepath.Join(dir, "dist", goos+"-"+goarch)
		if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // release binary dir read by upload-artifact step.
			return fmt.Errorf("mkdir %s: %w", outDir, err)
		}

		outName := fmt.Sprintf("%s-%s-%s", binaryName, goos, goarch)
		if goos == archGOOSWindows {
			outName += extExe
		}

		_, _ = fmt.Fprintf(w, "Building %s -> %s\n", triple, filepath.Join(outDir, outName))

		args := []string{
			subCmdBuild, "--release", flagCargoLocked,
			"--target", triple,
			// `--bin` defaults to the package's main binary; explicit
			// flag lets workspaces with multiple bins disambiguate, but
			// we omit it so cargo picks the package's default bin.
			"--target-dir", filepath.Join(dir, "target"),
		}
		if err := tool.Run(ctx, CargoRunInput{
			Dir:    dir,
			Args:   args,
			Stdout: w,
			Stderr: stderr,
		}); err != nil {
			return err
		}

		// Cargo writes target/<triple>/release/<crate_name>(.exe). Locate
		// it and copy to the dist/ layout. We use the crate name (which
		// may differ from --binary-name override) for the source; the
		// destination uses binaryName per the GHA output contract.
		built, err := locateCargoBinary(dir, triple, binaryName, goos)
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
//
//nolint:cyclop // input-validation flow mirrors GoMetadata.
func CargoMetadata(ctx context.Context, tool CargoTool, sink ci.OutputSink, w io.Writer, in CargoMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultCargoDir(in.Dir)

	meta, err := readCargoMetadata(ctx, tool, dir)
	if err != nil {
		return err
	}

	binaryName := firstNonEmpty(in.BinaryName, in.ArtifactName, meta.binaryName)
	if err := validateScalarValue(binaryName); err != nil {
		return fmt.Errorf("binary-name: %w", err)
	}

	version := domainversion.StripVPrefix(strings.TrimSpace(in.Version))
	if version == "" {
		version = domainversion.StripVPrefix(strings.TrimSpace(in.RefName))
	}

	if version != "" {
		if err := validateScalarValue(version); err != nil {
			return fmt.Errorf("version: %w", err)
		}
	}

	if version == "" {
		version = meta.version
	}

	if version == "" {
		version = "dev"
	}

	outputs := []struct{ key, value string }{
		{outKeyBinaryName, binaryName},
		{outKeyVersion, version},
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

	_, _ = fmt.Fprintf(w, "Binary: %s\nPackage: %s\nVersion: %s\n", binaryName, meta.packageName, version)

	return nil
}

// cargoMetadata is the subset of `cargo metadata --format-version 1`
// output we parse — enough to derive binary-name, version, package.
type cargoMetadata struct {
	binaryName  string
	version     string
	packageName string
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

	type pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Targets []struct {
			Name string   `json:"name"`
			Kind []string `json:"kind"`
		} `json:"targets"`
	}

	var doc struct {
		Packages       []pkg    `json:"packages"`
		WorkspaceRoot  string   `json:"workspace_root"`
		WorkspaceMems  []string `json:"workspace_members"` //nolint:tagliatelle // schema field is exactly this.
		Resolve        any      `json:"resolve"`
	}

	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&doc); err != nil {
		return cargoMetadata{}, fmt.Errorf("parse cargo metadata: %w", err)
	}

	if len(doc.Packages) == 0 {
		return cargoMetadata{}, fmt.Errorf("cargo metadata returned no packages: %w", errs.ErrInvalidConfig)
	}

	// Pick the root package — cargo metadata --no-deps lists workspace
	// members only, with the root first when there is a [package] at
	// workspace_root. Single-crate projects have exactly one entry.
	root := doc.Packages[0]

	// Find the package's [[bin]] target, if any. cargo's default rule:
	// the [[bin]] with name matching the package is the canonical binary.
	// Fall back to the package name when no [[bin]] is declared (cargo
	// still produces a binary at target/<triple>/release/<pkg>).
	binaryName := root.Name
	for _, t := range root.Targets {
		for _, k := range t.Kind {
			if k == "bin" && t.Name != "" {
				binaryName = t.Name

				break
			}
		}
	}

	return cargoMetadata{
		binaryName:  binaryName,
		version:     root.Version,
		packageName: root.Name,
	}, nil
}

// locateCargoBinary returns the path to the produced binary inside
// target/<triple>/release/. cargo names the binary after the [[bin]]
// target (or, by default, the package name). The override flow uses
// --binary-name to rename in dist/ but cargo still writes the source
// under its own naming; we search both candidates and the first match
// wins.
func locateCargoBinary(dir, triple, binaryName, goos string) (string, error) {
	suffix := ""
	if goos == archGOOSWindows {
		suffix = extExe
	}

	candidates := []string{
		filepath.Join(dir, "target", triple, "release", binaryName+suffix),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("compiled binary not found under target/%s/release/ (looked for %q): %w", triple, binaryName+suffix, errs.ErrInvalidConfig)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // caller-supplied path resolved within working-dir.
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // release binary executable bit required.
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}

	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}

	return nil
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
		return []string{"linux/amd64"}, nil
	}

	platforms := make([]string, 0)

	for _, raw := range strings.Split(value, ",") {
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
	parts := strings.Split(platform, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid platform %q, expected GOOS/GOARCH: %w", platform, errs.ErrUsage)
	}

	goos, goarch := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !domainbuild.IsKnownCargoPlatform(goos + "/" + goarch) {
		return "", "", fmt.Errorf("unsupported platform %q for cargo (no Rust target triple mapping): %w", platform, errs.ErrUsage)
	}

	return goos, goarch, nil
}
