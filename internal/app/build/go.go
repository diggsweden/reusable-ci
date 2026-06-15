// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domainbuild "github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/internal/domain/version"
)

// GoTool runs `go` commands.
//
// architecture uses narrow per-tool interfaces so adapters for `go`
// vs `cyclonedx-gomod` stay swappable. Merging would conflate roles.
//
//nolint:iface // intentionally distinct consumer-defined port — the
type GoTool interface {
	Run(ctx context.Context, in GoRunInput) error
}

// GoRunInput is one go/cyclonedx-gomod command invocation.
type GoRunInput = domainbuild.GoRunInput

// CycloneDXGoModTool runs cyclonedx-gomod commands.
//
//nolint:iface // see GoTool — distinct consumer-defined port by design.
type CycloneDXGoModTool interface {
	Run(ctx context.Context, in GoRunInput) error
}

// GoMetadataInput drives GoMetadata.
type GoMetadataInput struct {
	Dir             string
	ArtifactName    string
	BinaryNameInput string
	VersionInput    string
	RefName         string
}

// GoTestInput drives GoTest.
type GoTestInput struct {
	Dir       string
	BuildTags string
}

// GoBuildSBOMInput drives GoBuildSBOM.
type GoBuildSBOMInput struct {
	Dir          string
	ArtifactName string
	BinaryName   string
}

// GoDownload runs `go mod download`.
func GoDownload(ctx context.Context, tool GoTool, w, stderr io.Writer, dir string) error {
	return tool.Run(ctx, GoRunInput{Dir: defaultGoDir(dir), Args: []string{"mod", "download"}, Stdout: w, Stderr: stderr})
}

// GoTest runs `go test ./...` with optional build tags.
func GoTest(ctx context.Context, tool GoTool, w, stderr io.Writer, in GoTestInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	args := []string{"test"}
	if strings.TrimSpace(in.BuildTags) != "" {
		args = append(args, "-tags", strings.TrimSpace(in.BuildTags))
	}

	args = append(args, "./...")

	return tool.Run(ctx, GoRunInput{Dir: defaultGoDir(in.Dir), Args: args, Stdout: w, Stderr: stderr})
}

// GoBuildSBOM generates a build-layer SBOM into the canonical reusable-ci path.
func GoBuildSBOM(ctx context.Context, tool CycloneDXGoModTool, w, stderr io.Writer, in GoBuildSBOMInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultGoDir(in.Dir)

	name := firstNonEmpty(in.ArtifactName, in.BinaryName)
	if name == "" {
		return fmt.Errorf("artifact-name or binary-name is required: %w", errs.ErrUsage)
	}

	safeName := domainversion.SanitizePathToken(name)
	if safeName == "" {
		return fmt.Errorf("artifact-name/binary-name %q is not a valid path token: %w", name, errs.ErrUsage)
	}

	sbomDir := filepath.Join(".reusable-ci", "go-build-sbom", safeName)
	if err := os.MkdirAll(filepath.Join(dir, sbomDir), 0o755); err != nil { //nolint:gosec // bom.json read by workflow.
		return fmt.Errorf("mkdir go build sbom dir: %w", err)
	}

	return tool.Run(ctx, GoRunInput{
		Dir:    dir,
		Args:   []string{"mod", "-json", "-output", filepath.Join(sbomDir, "bom.json"), "."},
		Stdout: w,
		Stderr: stderr,
	})
}

// GoBuildBinariesInput drives GoBuildBinaries.
type GoBuildBinariesInput struct {
	Dir         string
	BinaryName  string
	BuildTags   string
	LDFlags     string
	MainPackage string
	Platforms   string
	// Version is the explicit version override. When empty, RefName is
	// consulted as a fallback; both are normalised through
	// domainversion.StripVPrefix so "v1.2.3" and "1.2.3" produce the
	// same baked-in result.
	Version string
	RefName string
	Commit  string
}

// GoBuildBinaries cross-compiles one binary per target platform into dist/.
//
//nolint:cyclop // matrix loop: GOOS×GOARCH×ldflags×outputName combinations.
func GoBuildBinaries(ctx context.Context, tool GoTool, w, stderr io.Writer, in GoBuildBinariesInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultGoDir(in.Dir)

	binaryName := strings.TrimSpace(in.BinaryName)
	if binaryName == "" {
		// Match `build go metadata`: derive from the module basename so
		// callers can rely on the same default everywhere.
		module, err := readGoModulePath(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}

		binaryName = filepath.Base(module)
	}

	// Resolve version with the same precedence as build go metadata:
	// --version → --ref-name → error. Both are normalised through
	// StripVPrefix so "v1.2.3" and "1.2.3" yield the same ldflag.
	version := domainversion.StripVPrefix(strings.TrimSpace(in.Version))
	if version == "" {
		version = domainversion.StripVPrefix(strings.TrimSpace(in.RefName))
	}

	if version == "" {
		// Empty injection would silently produce `main.version=""` in
		// the binary — indistinguishable from a broken build. Reject so
		// CI callers see the omission explicitly.
		return fmt.Errorf("version is required (pass --version or --ref-name, or set $VERSION or $REF_NAME; use 'dev' for local builds): %w", errs.ErrUsage)
	}

	if err := validateScalarValue(version); err != nil {
		return fmt.Errorf("version: %w", err)
	}

	platforms, err := splitGoPlatforms(in.Platforms)
	if err != nil {
		return err
	}

	mainPackage := strings.TrimSpace(in.MainPackage)
	if mainPackage == "" {
		mainPackage = "."
	}

	// Wipe only the per-platform output dirs we are about to write.
	// A blanket RemoveAll(dist) would silently delete sibling artifacts
	// the user (or another tool) staged in dist/ — e.g. tarballs,
	// release notes, SBOMs. Targeted cleanup keeps the contract narrow:
	// `build go compile` owns its own platform subdirectories.
	for _, platform := range platforms {
		goos, goarch, _ := parseGoPlatform(platform)
		if rmErr := os.RemoveAll(filepath.Join(dir, "dist", goos+"-"+goarch)); rmErr != nil {
			return fmt.Errorf("remove dist/%s-%s: %w", goos, goarch, rmErr)
		}
	}

	buildDate, err := resolveBuildDate(time.Now)
	if err != nil {
		return err
	}

	baseLDFlags := fmt.Sprintf("-s -w -X main.version=%s -X main.commit=%s -X main.date=%s", version, in.Commit, buildDate)
	if strings.TrimSpace(in.LDFlags) != "" {
		baseLDFlags += " " + strings.TrimSpace(in.LDFlags)
	}

	for _, platform := range platforms {
		goos, goarch, _ := parseGoPlatform(platform)

		outDir := filepath.Join(dir, "dist", goos+"-"+goarch)
		if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // release binary dir read by upload-artifact step.
			return fmt.Errorf("mkdir %s: %w", outDir, err)
		}

		outName := fmt.Sprintf("%s-%s-%s", binaryName, goos, goarch)
		if goos == archGOOSWindows {
			outName += extExe
		}

		_, _ = fmt.Fprintf(w, "Building %s/%s -> %s\n", goos, goarch, filepath.Join(outDir, outName))

		args := []string{subCmdBuild, "-trimpath", "-buildvcs=false", "-ldflags", baseLDFlags}
		if strings.TrimSpace(in.BuildTags) != "" {
			args = append(args, "-tags", strings.TrimSpace(in.BuildTags))
		}

		args = append(args, "-o", filepath.Join(outDir, outName), mainPackage)
		if err := tool.Run(ctx, GoRunInput{
			Dir:    dir,
			Env:    []string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=" + goarch},
			Args:   args,
			Stdout: w,
			Stderr: stderr,
		}); err != nil {
			return err
		}
	}

	return nil
}

// GoMetadata reads go.mod metadata and emits the build contract used by the Go
// build workflow: binary-name, version, and module.
//
//nolint:cyclop // input-validation flow: resolve dir → read module → resolve names → validate scalars → emit.
func GoMetadata(ctx context.Context, sink ci.OutputSink, w io.Writer, in GoMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	module, err := readGoModulePath(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}

	moduleBase := filepath.Base(module)
	binaryName := firstNonEmpty(in.BinaryNameInput, in.ArtifactName, moduleBase)

	if err := validateScalarValue(binaryName); err != nil {
		return fmt.Errorf("binary-name: %w", err)
	}

	// Both --version and --ref-name are commonly populated from git
	// tags (which carry the "v" prefix); both inputs are normalised the
	// same way so callers get identical output regardless of which
	// flag they used.
	version := domainversion.StripVPrefix(strings.TrimSpace(in.VersionInput))
	if version == "" {
		version = domainversion.StripVPrefix(strings.TrimSpace(in.RefName))
	}

	if version != "" {
		if err := validateScalarValue(version); err != nil {
			return fmt.Errorf("version: %w", err)
		}
	}

	if version == "" {
		version = "dev"
	}

	// Use a slice of pairs (not a map) so the emission order is
	// deterministic — Go map iteration is randomised, which made
	// `--json` output diff-noisy across runs.
	outputs := []struct{ key, value string }{
		{"binary-name", binaryName},
		{"version", version}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"module", module},
	}
	for _, out := range outputs {
		if err := sink.Set(ctx, out.key, out.value); err != nil {
			// The sink already includes its own context (e.g. $GITHUB_OUTPUT
			// path); only add the key when the sink error is opaque.
			if errors.Is(err, errs.ErrInvalidConfig) || errors.Is(err, errs.ErrValidation) {
				return err
			}

			return fmt.Errorf("emit output %q: %w", out.key, err)
		}
	}

	_, _ = fmt.Fprintf(w, "Binary: %s\nModule: %s\nVersion: %s\n", binaryName, module, version)

	return nil
}

func readGoModulePath(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec,varnamelen // caller passes a CLI-flag-derived path to go.mod.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("go.mod not found at %q: %w", path, errs.ErrMissingInput)
		}

		return "", fmt.Errorf("read go.mod at %q: %w", path, err)
	}

	defer func() { _ = f.Close() }()

	s := bufio.NewScanner(f) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "module ") {
			module := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if module == "" {
				return "", fmt.Errorf("go.mod module path is empty: %w", errs.ErrInvalidConfig)
			}

			return module, nil
		}
	}

	if err := s.Err(); err != nil {
		return "", fmt.Errorf("scan go.mod: %w", err)
	}

	return "", fmt.Errorf("go.mod module directive not found: %w", errs.ErrInvalidConfig)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func defaultGoDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "."
	}

	return dir
}

func splitGoPlatforms(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{domainbuild.DefaultPlatform}, nil
	}

	platforms := make([]string, 0)

	for _, raw := range strings.Split(value, ",") {
		platform := strings.TrimSpace(raw)
		if platform == "" {
			continue
		}

		if _, _, err := parseGoPlatform(platform); err != nil {
			return nil, err
		}

		platforms = append(platforms, platform)
	}

	if len(platforms) == 0 {
		return nil, fmt.Errorf("platforms is empty: %w", errs.ErrUsage)
	}

	return platforms, nil
}

func parseGoPlatform(platform string) (string, string, error) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("invalid platform %q, expected GOOS/GOARCH: %w", platform, errs.ErrUsage)
	}

	goos, goarch := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if !domainbuild.IsKnownGoPlatform(goos + "/" + goarch) {
		return "", "", fmt.Errorf("unknown GOOS/GOARCH %q (not in `go tool dist list` for the pinned toolchain): %w", platform, errs.ErrUsage)
	}

	return goos, goarch, nil
}

// validateScalarValue rejects characters that break the GHA $GITHUB_OUTPUT
// scalar contract (which forbids \r\n in values) and that would allow
// trivial injection of fake output lines. The check runs at the app
// layer so the same input is rejected uniformly across all sinks
// (GHA, GitLab dotenv, JSON, /dev/null) — preventing the surprise of
// "works locally with --json, fails in CI with GHA sink".
func validateScalarValue(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("value contains a newline (would break output sinks): %w", errs.ErrUsage)
	}

	return nil
}

// resolveBuildDate returns the timestamp baked into the binary's
// `main.date` ldflag. SOURCE_DATE_EPOCH (seconds since UNIX epoch,
// reproducible-builds.org convention) wins when set so two identical
// invocations produce byte-identical binaries; the supplied now()
// fallback (typically time.Now) provides a useful default for
// non-reproducible local builds. The clock is injected for testability.
func resolveBuildDate(now func() time.Time) (string, error) {
	if raw := strings.TrimSpace(os.Getenv("SOURCE_DATE_EPOCH")); raw != "" {
		secs, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid SOURCE_DATE_EPOCH %q (expected integer seconds since UNIX epoch): %w", raw, errs.ErrUsage)
		}

		return time.Unix(secs, 0).UTC().Format(time.RFC3339), nil
	}

	return now().UTC().Format(time.RFC3339), nil
}
