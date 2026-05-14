// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

// BumpInput drives Bump.
type BumpInput struct {
	ProjectType projecttype.Type
	Version     string
	WorkingDir  string // empty → cwd

	// GradleVersionFile is the path to the gradle properties file
	// (relative to WorkingDir). Empty → "gradle.properties".
	GradleVersionFile string
	// XcconfigFile is the .xcconfig path. Empty → "versions.xcconfig".
	XcconfigFile string

	// MavenCLIOpts is the value of $MAVEN_CLI_OPTS, split into argv.
	MavenCLIOpts []string
}

// MavenOps abstracts the maven adapter for dependency injection. The
// dir-aware RunInheritIn is used (rather than RunInherit) so the bump
// can run mvn in the project's working directory without mutating
// global cwd.
type MavenOps interface {
	RunInheritIn(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error
}

// NPMOps abstracts the npm adapter.
type NPMOps interface {
	RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error
}

// CargoOps abstracts the cargo adapter.
type CargoOps interface {
	Available() bool
	RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error
}

// BumpOps bundles the three external-binary adapters Bump needs. Tests
// inject fakes; production wires the real adapters.
type BumpOps struct {
	Maven MavenOps
	NPM   NPMOps
	Cargo CargoOps
}

// Bump rewrites the version-of-record for a project per its
// project-type, optionally invoking maven/npm/cargo to refresh
// dependent files. Mirrors scripts/version/bump-version.sh.
func Bump(ctx context.Context, ops BumpOps, stdout, stderr io.Writer, annot output.Annotator, in BumpInput) error {
	if in.Version == "" {
		return fmt.Errorf("version is required: %w", errs.ErrUsage)
	}

	dir := in.WorkingDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	gradleFile := in.GradleVersionFile
	if gradleFile == "" {
		gradleFile = "gradle.properties"
	}
	xcconfig := in.XcconfigFile
	if xcconfig == "" {
		xcconfig = "versions.xcconfig"
	}

	fmt.Fprintf(stdout, "Bumping version to %s for %s project in %s\n", in.Version, in.ProjectType, dir)

	switch in.ProjectType {
	case projecttype.Maven:
		return bumpMaven(ctx, ops.Maven, dir, in.MavenCLIOpts, in.Version, stdout, stderr)
	case projecttype.NPM:
		return bumpNPM(ctx, ops.NPM, dir, in.Version, stdout, stderr)
	case projecttype.Gradle:
		return bumpGradleJVM(filepath.Join(dir, gradleFile), in.Version, stdout, annot)
	case projecttype.GradleAndroid:
		return bumpGradleAndroid(filepath.Join(dir, gradleFile), in.Version, stdout, annot)
	case projecttype.XcodeIOS:
		return bumpXcodeIOS(filepath.Join(dir, xcconfig), in.Version, stdout)
	case projecttype.Cargo:
		return bumpCargo(ctx, ops.Cargo, dir, in.Version, stdout, stderr, annot)
	case projecttype.Meta:
		fmt.Fprintln(stdout, "Meta project type - no version file to update")
		fmt.Fprintf(stdout, "✓ Version %s recorded for changelog generation only\n", in.Version)
		return nil
	default:
		return fmt.Errorf("Unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}
}

func bumpMaven(ctx context.Context, ops MavenOps, dir string, cliOpts []string, ver string, stdout, stderr io.Writer) error {
	if ops == nil {
		return errors.New("maven adapter not provided")
	}
	fmt.Fprintf(stdout, "Updating Maven version to %s\n", ver)
	args := make([]string, 0, len(cliOpts)+5)
	args = append(args, cliOpts...)
	args = append(args,
		"versions:set",
		"-DnewVersion="+ver,
		"-DgenerateBackupPoms=false",
		"-DprocessAllModules=true",
		"-DskipTests",
	)
	if err := ops.RunInheritIn(ctx, dir, stdout, stderr, args...); err != nil {
		return fmt.Errorf("mvn versions:set: %w", err)
	}
	fmt.Fprintln(stdout, "✓ Maven version updated (including all sub-modules)")
	return nil
}

func bumpNPM(ctx context.Context, ops NPMOps, dir, ver string, stdout, stderr io.Writer) error {
	if ops == nil {
		return errors.New("npm adapter not provided")
	}
	fmt.Fprintf(stdout, "Updating NPM version to %s\n", ver)
	if err := ops.RunInherit(ctx, dir, stdout, stderr,
		"version", ver, "--no-git-tag-version", "--allow-same-version"); err != nil {
		return fmt.Errorf("npm version: %w", err)
	}
	fmt.Fprintln(stdout, "✓ NPM version updated")
	return nil
}

// readGradleVersionFile reads path, emitting a GHA `::error::` line to
// stderr when missing (matches the bash script's user-visible shape) and
// returning a wrapped error either way. Shared by bumpGradleJVM and
// bumpGradleAndroid, which used to duplicate the entire missing-file
// arm verbatim.
func readGradleVersionFile(path string, annot output.Annotator) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		annot.Errorf("Gradle version file not found: %s", path)
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return body, nil
}

func bumpGradleJVM(path, ver string, stdout io.Writer, annot output.Annotator) error {
	body, err := readGradleVersionFile(path, annot)
	if err != nil {
		return err
	}
	out, res := version.UpdateGradleJVMVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		fmt.Fprintf(stdout, "Updated version to %s\n", ver)
	} else {
		fmt.Fprintf(stdout, "Added version=%s\n", ver)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(stdout, "✓ Gradle JVM version updated")
	fmt.Fprint(stdout, out)
	return nil
}

func bumpGradleAndroid(path, ver string, stdout io.Writer, annot output.Annotator) error {
	body, err := readGradleVersionFile(path, annot)
	if err != nil {
		return err
	}
	out, res := version.UpdateOrAddProperty(string(body), "versionName", ver, "=")
	if res == version.UpdatePropertyUpdated {
		fmt.Fprintf(stdout, "Updated versionName to %s\n", ver)
	} else {
		fmt.Fprintf(stdout, "Added versionName=%s\n", ver)
	}

	res2 := version.IncrementVersionCode(out)
	out = res2.Body
	if res2.Added {
		fmt.Fprintln(stdout, "Added versionCode=1")
	} else {
		fmt.Fprintf(stdout, "Incremented versionCode: %d → %d\n", res2.Old, res2.New)
	}

	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(stdout, "✓ Gradle Android version updated")
	fmt.Fprint(stdout, out)
	return nil
}

func bumpXcodeIOS(path, ver string, stdout io.Writer) error {
	body, err := os.ReadFile(path)
	if err != nil {
		// Bash creates the file with MARKETING_VERSION when missing.
		fmt.Fprintf(stdout, "Creating %s\n", path)
		out := "MARKETING_VERSION = " + ver + "\n"
		if werr := os.WriteFile(path, []byte(out), 0o644); werr != nil {
			return fmt.Errorf("create %s: %w", path, werr)
		}
		fmt.Fprintf(stdout, "✓ Created %s with MARKETING_VERSION = %s\n", path, ver)
		return nil
	}
	out, res := version.UpdateXcodeMarketingVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		fmt.Fprintf(stdout, "Updated MARKETING_VERSION to %s\n", ver)
	} else {
		fmt.Fprintf(stdout, "Added MARKETING_VERSION = %s\n", ver)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(stdout, "✓ Xcode version updated")
	fmt.Fprint(stdout, out)
	return nil
}

func bumpCargo(ctx context.Context, ops CargoOps, dir, ver string, stdout, stderr io.Writer, annot output.Annotator) error {
	cargoToml := filepath.Join(dir, "Cargo.toml")
	body, err := os.ReadFile(cargoToml)
	if err != nil {
		annot.Errorf("Cargo.toml not found in %s", dir)
		return fmt.Errorf("read %s: %w", cargoToml, err)
	}
	out, sec, err := version.UpdateCargoVersion(string(body), ver)
	if err != nil {
		annot.Errorf("%s", err.Error())
		return err
	}
	if err := os.WriteFile(cargoToml, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cargoToml, err)
	}
	switch sec {
	case version.CargoSectionWorkspacePackage:
		fmt.Fprintf(stdout, "Updated [workspace.package].version to %s\n", ver)
	case version.CargoSectionPackage:
		fmt.Fprintf(stdout, "Updated [package].version to %s\n", ver)
	}

	// Refresh Cargo.lock if present and cargo is available.
	cargoLock := filepath.Join(dir, "Cargo.lock")
	if _, err := os.Stat(cargoLock); err == nil {
		if ops == nil || !ops.Available() {
			fmt.Fprintln(stdout, "Warning: cargo not found; Cargo.lock not refreshed (will self-heal on next cargo invocation)")
		} else {
			if err := ops.RunInherit(ctx, dir, stdout, stderr, "update", "--workspace", "--offline"); err != nil {
				// Try without --offline.
				if err2 := ops.RunInherit(ctx, dir, stdout, stderr, "update", "--workspace"); err2 != nil {
					fmt.Fprintln(stdout, "Warning: failed to refresh Cargo.lock; will be regenerated on next cargo invocation")
				}
			}
		}
	}
	fmt.Fprintln(stdout, "✓ Rust version updated")
	return nil
}
