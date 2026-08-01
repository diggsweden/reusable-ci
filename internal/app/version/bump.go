// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
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
	RunInheritIn(ctx context.Context, dir string, w, stderr io.Writer, args ...string) error
}

// NPMOps abstracts the npm adapter.
type NPMOps interface {
	RunInherit(ctx context.Context, dir string, w, stderr io.Writer, args ...string) error
}

// CargoOps abstracts the cargo adapter.
type CargoOps interface {
	Available() bool
	RunInherit(ctx context.Context, dir string, w, stderr io.Writer, args ...string) error
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
// dependent files.
//
//nolint:cyclop // version-bump flow: read manifest → compute next → write per project type.
func Bump(ctx context.Context, ops BumpOps, w, stderr io.Writer, annot output.Annotator, in BumpInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	_, _ = fmt.Fprintf(w, "Bumping version to %s for %s project in %s\n", in.Version, in.ProjectType, dir)

	switch in.ProjectType {
	case projecttype.Maven:
		return bumpMaven(ctx, ops.Maven, dir, in.MavenCLIOpts, in.Version, w, stderr)
	case projecttype.NPM:
		return bumpNPM(ctx, ops.NPM, dir, in.Version, w, stderr)
	case projecttype.Gradle:
		return bumpGradleJVM(filepath.Join(dir, gradleFile), in.Version, w, annot)
	case projecttype.GradleAndroid:
		return bumpGradleAndroid(filepath.Join(dir, gradleFile), in.Version, w, annot)
	case projecttype.XcodeIOS:
		return bumpXcodeIOS(filepath.Join(dir, xcconfig), in.Version, w)
	case projecttype.Go:
		_, _ = fmt.Fprintln(w, "Go project type - no version file to update")
		_, _ = fmt.Fprintf(w, "%s Version %s will be supplied by the release tag/build ldflags\n", clicolor.Check(w), in.Version)

		return nil
	case projecttype.Cargo:
		return bumpCargo(ctx, ops.Cargo, dir, in.Version, w, stderr, annot)
	case projecttype.Meta:
		_, _ = fmt.Fprintln(w, "Meta project type - no version file to update")
		_, _ = fmt.Fprintf(w, "%s Version %s recorded for changelog generation only\n", clicolor.Check(w), in.Version)

		return nil
	default:
		return fmt.Errorf("unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}
}

func bumpMaven(ctx context.Context, ops MavenOps, dir string, cliOpts []string, ver string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if ops == nil {
		return fmt.Errorf("maven adapter not provided"+": %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(w, "Updating Maven version to %s\n", ver)

	args := make([]string, 0, len(cliOpts)+5)
	args = append(args, cliOpts...)

	args = append(args,
		"versions:set",
		"-DnewVersion="+ver,
		"-DgenerateBackupPoms=false",
		"-DprocessAllModules=true",
		"-DskipTests",
	)
	// Adapter already labels its error with the binary + subcommand;
	// wrapping again here would double-prefix.
	if err := ops.RunInheritIn(ctx, dir, w, stderr, args...); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "%s Maven version updated (including all sub-modules)\n", clicolor.Check(w))

	return nil
}

func bumpNPM(ctx context.Context, ops NPMOps, dir, ver string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if ops == nil {
		return fmt.Errorf("npm adapter not provided"+": %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(w, "Updating NPM version to %s\n", ver)

	if err := ops.RunInherit(ctx, dir, w, stderr,
		"version", ver, "--no-git-tag-version", "--allow-same-version"); err != nil {
		return fmt.Errorf("npm version: %w", err)
	}

	_, _ = fmt.Fprintf(w, "%s NPM version updated\n", clicolor.Check(w))

	return nil
}

// readGradleVersionFile reads path, emitting a format-aware error
// annotation to stderr when missing (::error:: on GitHub, a plain Error:
// line elsewhere incl. Forgejo) and returning a wrapped error either way.
// Shared by bumpGradleJVM and bumpGradleAndroid, which used to duplicate
// the entire missing-file arm verbatim.
func readGradleVersionFile(path string, annot output.Annotator) ([]byte, error) {
	body, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value.
	if err != nil {
		annot.Errorf("Gradle version file not found: %s", path)

		// The gradle properties file is user-supplied project input, so a
		// missing/unreadable one is EX_NOINPUT (66), not the unclassified
		// internal-bug default (70). Keep the underlying os error wrapped
		// too so debug output still shows the real cause.
		return nil, fmt.Errorf("read %s: %w: %w", path, err, errs.ErrMissingInput)
	}

	return body, nil
}

func bumpGradleJVM(path, ver string, w io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := readGradleVersionFile(path, annot)
	if err != nil {
		return err
	}

	out, res := version.UpdateGradleJVMVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated version to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added version=%s\n", ver)
	}

	if err := os.WriteFile(path, []byte(out), 0o644); err != nil { //nolint:gosec // project file; ecosystem tools expect 0644.
		return fmt.Errorf("write %s: %w", path, err)
	}

	_, _ = fmt.Fprintf(w, "%s Gradle JVM version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpGradleAndroid(path, ver string, w io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := readGradleVersionFile(path, annot)
	if err != nil {
		return err
	}

	out, res := version.UpdateOrAddProperty(string(body), "versionName", ver, "=")
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated versionName to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added versionName=%s\n", ver)
	}

	res2 := version.IncrementVersionCode(out)

	out = res2.Body
	if res2.Added {
		_, _ = fmt.Fprintln(w, "Added versionCode=1")
	} else {
		_, _ = fmt.Fprintf(w, "Incremented versionCode: %d → %d\n", res2.Old, res2.New)
	}

	if err := os.WriteFile(path, []byte(out), 0o644); err != nil { //nolint:gosec // project file; ecosystem tools expect 0644.
		return fmt.Errorf("write %s: %w", path, err)
	}

	_, _ = fmt.Fprintf(w, "%s Gradle Android version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpXcodeIOS(path, ver string, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value.
	if err != nil {
		// Bash creates the file with MARKETING_VERSION when missing.
		_, _ = fmt.Fprintf(w, "Creating %s\n", path)

		out := "MARKETING_VERSION = " + ver + "\n"
		if werr := os.WriteFile(path, []byte(out), 0o644); werr != nil { //nolint:gosec // xcconfig file; xcodebuild expects 0644.
			return fmt.Errorf("create %s: %w", path, werr)
		}

		_, _ = fmt.Fprintf(w, "%s Created %s with MARKETING_VERSION = %s\n", clicolor.Check(w), path, ver)

		return nil
	}

	out, res := version.UpdateXcodeMarketingVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated MARKETING_VERSION to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added MARKETING_VERSION = %s\n", ver)
	}

	if err := os.WriteFile(path, []byte(out), 0o644); err != nil { //nolint:gosec // project file; ecosystem tools expect 0644.
		return fmt.Errorf("write %s: %w", path, err)
	}

	_, _ = fmt.Fprintf(w, "%s Xcode version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpCargo(ctx context.Context, ops CargoOps, dir, ver string, w, stderr io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	cargoToml := filepath.Join(dir, "Cargo.toml")

	body, err := os.ReadFile(cargoToml) //nolint:gosec // cargoToml is dir+"Cargo.toml".
	if err != nil {
		annot.Errorf("Cargo.toml not found in %s", dir)

		// Cargo.toml is user-supplied project input — a missing/unreadable
		// one is EX_NOINPUT (66), not the unclassified internal-bug default
		// (70). The underlying os error stays wrapped for debug output.
		return fmt.Errorf("read %s: %w: %w", cargoToml, err, errs.ErrMissingInput)
	}

	out, sec, err := version.UpdateCargoVersion(string(body), ver)
	if err != nil {
		annot.Errorf("%s", err.Error())

		return err
	}

	if err := os.WriteFile(cargoToml, []byte(out), 0o644); err != nil { //nolint:gosec // Cargo.toml; cargo tools expect 0644.
		return fmt.Errorf("write %s: %w", cargoToml, err)
	}

	switch sec {
	case version.CargoSectionWorkspacePackage:
		_, _ = fmt.Fprintf(w, "Updated [workspace.package].version to %s\n", ver)
	case version.CargoSectionPackage:
		_, _ = fmt.Fprintf(w, "Updated [package].version to %s\n", ver)
	case version.CargoSectionNone:
		// Unreachable in practice — BumpCargoVersion above returns an
		// error before we get here when no version line was found.
	}

	refreshCargoLock(ctx, ops, dir, w, stderr)

	_, _ = fmt.Fprintf(w, "%s Rust version updated\n", clicolor.Check(w))

	return nil
}

// refreshCargoLock re-resolves Cargo.lock after a version bump if it
// exists and cargo is available. Failures are warnings — cargo will
// regenerate the lockfile on the next real invocation.
func refreshCargoLock(ctx context.Context, ops CargoOps, dir string, w, stderr io.Writer) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	cargoLock := filepath.Join(dir, "Cargo.lock")
	if _, err := os.Stat(cargoLock); err != nil {
		return
	}

	if ops == nil || !ops.Available() {
		_, _ = fmt.Fprintln(w, "Warning: cargo not found; Cargo.lock not refreshed (will self-heal on next cargo invocation)")

		return
	}

	if err := ops.RunInherit(ctx, dir, w, stderr, "update", "--workspace", "--offline"); err == nil {
		return
	}
	// Try without --offline.
	if err := ops.RunInherit(ctx, dir, w, stderr, "update", "--workspace"); err != nil {
		_, _ = fmt.Fprintln(w, "Warning: failed to refresh Cargo.lock; will be regenerated on next cargo invocation")
	}
}
