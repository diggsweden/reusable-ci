// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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

// bumpTarget keeps the engine-owned filename identical during preflight, writes
// and path reporting. Native Maven/npm target selection is deliberately absent.
type bumpTarget struct {
	BumpInput
	file string
}

func normalizeBumpTarget(in BumpInput) (bumpTarget, error) { //nolint:cyclop // one target-selection switch followed by shared serialization and path checks.
	file := ""

	switch in.ProjectType {
	case projecttype.Gradle, projecttype.GradleAndroid:
		file = in.GradleVersionFile
		if file == "" {
			file = "gradle.properties"
		}
	case projecttype.XcodeIOS:
		file = in.XcconfigFile
		if file == "" {
			file = "versions.xcconfig"
		}
	case projecttype.Cargo:
		file = "Cargo.toml"
	case projecttype.Go, projecttype.Meta, projecttype.Maven, projecttype.NPM:
	default:
		return bumpTarget{}, fmt.Errorf("unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}

	if file != "" {
		// Check raw input before trimming can hide a forbidden line boundary.
		if strings.ContainsAny(in.Version, "\r\n\x00") {
			return bumpTarget{}, fmt.Errorf("version must not contain CR, LF or NUL: %w", errs.ErrUsage)
		}

		if !utf8.ValidString(in.Version) {
			return bumpTarget{}, fmt.Errorf("version must be valid UTF-8: %w", errs.ErrUsage)
		}

		if !pathsafe.Relative(file) || filepath.Clean(file) == "." || strings.ContainsRune(file, '\x00') {
			return bumpTarget{}, fmt.Errorf("version file must be project-relative and contain no NUL: %w", errs.ErrUsage)
		}

		file = filepath.Clean(file)
	}

	in.Version = strings.TrimSpace(in.Version)
	if in.Version == "" {
		return bumpTarget{}, fmt.Errorf("version is required: %w", errs.ErrUsage)
	}

	if parsed, ok := version.ParseSemver(in.Version); ok {
		in.Version = parsed.Version
	}

	return bumpTarget{BumpInput: in, file: file}, nil
}

// Bump rewrites the version-of-record for a project per its
// project-type, optionally invoking maven/npm/cargo to refresh
// dependent files.
//
//nolint:cyclop // version-bump flow: read manifest → compute next → write per project type.
func Bump(ctx context.Context, ops BumpOps, w, stderr io.Writer, annot output.Annotator, in BumpInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	target, err := normalizeBumpTarget(in)
	if err != nil {
		return err
	}

	in = target.BumpInput

	dir := in.WorkingDir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	_, _ = fmt.Fprintf(w, "Bumping version to %s for %s project in %s\n", in.Version, in.ProjectType, dir)

	switch in.ProjectType {
	case projecttype.Maven:
		return bumpMaven(ctx, ops.Maven, dir, in.MavenCLIOpts, in.Version, w, stderr)
	case projecttype.NPM:
		return bumpNPM(ctx, ops.NPM, dir, in.Version, w, stderr)
	case projecttype.Gradle:
		return withVersionRoot(dir, target.file, func(root *os.Root, relative, display string) error {
			return bumpGradleJVM(root, relative, display, in.Version, w, annot)
		})
	case projecttype.GradleAndroid:
		return withVersionRoot(dir, target.file, func(root *os.Root, relative, display string) error {
			return bumpGradleAndroid(root, relative, display, in.Version, w, annot)
		})
	case projecttype.XcodeIOS:
		return withVersionRoot(dir, target.file, func(root *os.Root, relative, display string) error {
			return bumpXcodeIOS(root, relative, display, in.Version, w)
		})
	case projecttype.Go:
		_, _ = fmt.Fprintln(w, "Go project type - no version file to update")
		_, _ = fmt.Fprintf(w, "%s Version %s will be supplied by the release tag/build ldflags\n", clicolor.Check(w), in.Version)

		return nil
	case projecttype.Cargo:
		return withVersionRoot(dir, target.file, func(root *os.Root, relative, display string) error {
			return bumpCargo(ctx, ops.Cargo, root, relative, display, dir, in.Version, w, stderr, annot)
		})
	case projecttype.Meta:
		_, _ = fmt.Fprintln(w, "Meta project type - no version file to update")
		_, _ = fmt.Fprintf(w, "%s Version %s recorded for changelog generation only\n", clicolor.Check(w), in.Version)

		return nil
	default:
		return fmt.Errorf("unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}
}

func withVersionRoot(dir, relative string, run func(*os.Root, string, string) error) error {
	if !pathsafe.Relative(relative) {
		return fmt.Errorf("version file must be relative to the project directory: %q: %w", relative, errs.ErrUsage)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open project directory %s: %w: %w", dir, err, errs.ErrMissingInput)
	}
	defer func() { _ = root.Close() }()

	return run(root, filepath.Clean(relative), filepath.Join(dir, relative))
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
// Shared by bumpGradleJVM and bumpGradleAndroid.
//
// The read goes through the project's os.Root, which has three distinct
// failure modes an operator has to be able to tell apart:
//
//   - the file is absent — their path is wrong (EX_NOINPUT);
//   - the file is unreadable — a permission problem (EX_NOPERM);
//   - the path resolved outside the project directory, most commonly a
//     symlink pointing out of the checkout. os.Root refuses that, and it is a
//     refusal rather than a missing file.
//
// All three used to be reported as "Gradle version file not found" with
// EX_NOINPUT. For the escape that described a file the operator can see with
// `ls`, and buried the one detail worth surfacing: something in the tree
// points out of it.
func readGradleVersionFile(root *os.Root, relative, display string, annot output.Annotator) ([]byte, error) {
	body, err := root.ReadFile(relative)
	if err == nil {
		return body, nil
	}

	// Keep the underlying os error wrapped in every branch so debug output
	// still shows the real cause.
	switch {
	case errors.Is(err, fs.ErrNotExist):
		annot.Errorf("Gradle version file not found: %s", display)

		return nil, fmt.Errorf("read %s: %w: %w", display, err, errs.ErrMissingInput)

	case errors.Is(err, fs.ErrPermission):
		annot.Errorf("Gradle version file is not readable: %s", display)

		return nil, fmt.Errorf("read %s: %w: %w", display, err, errs.ErrPermissionDenied)
	}

	annot.Errorf("Gradle version file resolves outside the project directory: %s", display)

	return nil, fmt.Errorf("read %s: %w: %w", display, err, errs.ErrValidation)
}

func bumpGradleJVM(root *os.Root, relative, display, ver string, w io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := readGradleVersionFile(root, relative, display, annot)
	if err != nil {
		return err
	}

	out, res := version.UpdateGradleJVMVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated version to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added version=%s\n", ver)
	}

	if err := root.WriteFile(relative, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", display, err)
	}

	_, _ = fmt.Fprintf(w, "%s Gradle JVM version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpGradleAndroid(root *os.Root, relative, display, ver string, w io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := readGradleVersionFile(root, relative, display, annot)
	if err != nil {
		return err
	}

	// Properties interpret backslashes as escapes, including line continuation.
	out, res := version.UpdateOrAddProperty(string(body), "versionName", strings.ReplaceAll(ver, `\`, `\\`), "=")
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated versionName to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added versionName=%s\n", ver)
	}

	res2 := version.IncrementVersionCode(out)
	// Setting the requested version again is a retry, not a new Android release.
	if out == string(body) && !res2.Added {
		return nil
	}

	out = res2.Body
	if res2.Added {
		_, _ = fmt.Fprintln(w, "Added versionCode=1")
	} else {
		_, _ = fmt.Fprintf(w, "Incremented versionCode: %d → %d\n", res2.Old, res2.New)
	}

	if err := root.WriteFile(relative, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", display, err)
	}

	_, _ = fmt.Fprintf(w, "%s Gradle Android version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpXcodeIOS(root *os.Root, relative, display, ver string, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := root.ReadFile(relative)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %w", display, err)
		}

		// Bash creates the file with MARKETING_VERSION when missing.
		_, _ = fmt.Fprintf(w, "Creating %s\n", display)

		out := "MARKETING_VERSION = " + ver + "\n"
		if werr := root.WriteFile(relative, []byte(out), 0o644); werr != nil {
			return fmt.Errorf("create %s: %w", display, werr)
		}

		_, _ = fmt.Fprintf(w, "%s Created %s with MARKETING_VERSION = %s\n", clicolor.Check(w), display, ver)

		return nil
	}

	out, res := version.UpdateXcodeMarketingVersion(string(body), ver)
	if res == version.UpdatePropertyUpdated {
		_, _ = fmt.Fprintf(w, "Updated MARKETING_VERSION to %s\n", ver)
	} else {
		_, _ = fmt.Fprintf(w, "Added MARKETING_VERSION = %s\n", ver)
	}

	if err := root.WriteFile(relative, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", display, err)
	}

	_, _ = fmt.Fprintf(w, "%s Xcode version updated\n", clicolor.Check(w))
	_, _ = fmt.Fprint(w, out)

	return nil
}

func bumpCargo(ctx context.Context, ops CargoOps, root *os.Root, relative, display, dir, ver string, w, stderr io.Writer, annot output.Annotator) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := root.ReadFile(relative)
	if err != nil {
		annot.Errorf("Cargo.toml not found in %s", dir)

		// Cargo.toml is user-supplied project input — a missing/unreadable
		// one is EX_NOINPUT (66), not the unclassified internal-bug default
		// (70). The underlying os error stays wrapped for debug output.
		return fmt.Errorf("read %s: %w: %w", display, err, errs.ErrMissingInput)
	}

	out, sec, err := version.UpdateCargoVersion(string(body), ver)
	if err != nil {
		annot.Errorf("%s", err.Error())

		return err
	}

	if err := root.WriteFile(relative, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", display, err)
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

	refreshCargoLock(ctx, ops, root, dir, w, stderr)

	_, _ = fmt.Fprintf(w, "%s Rust version updated\n", clicolor.Check(w))

	return nil
}

// refreshCargoLock re-resolves Cargo.lock after a version bump if it
// exists and cargo is available. Failures are warnings — cargo will
// regenerate the lockfile on the next real invocation.
func refreshCargoLock(ctx context.Context, ops CargoOps, root *os.Root, dir string, w, stderr io.Writer) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if _, err := root.Stat("Cargo.lock"); err != nil {
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
