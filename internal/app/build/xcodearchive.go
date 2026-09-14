// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// XcodeBuildOps abstracts the xcodebuild adapter.
type XcodeBuildOps interface {
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) (int, error)
}

// XcodeArchiveInput drives XcodeArchive.
type XcodeArchiveInput struct {
	Workspace     string
	Project       string
	Scheme        string
	Configuration string
	Destination   string
	XcconfigPath  string
	BuildNumber   string
}

// XcodeArchive runs `xcodebuild archive ...` with the appropriate
// flag composition.
//
// The bash piped to `xcbeautify`; the Go port streams xcodebuild's
// own output through (callers can pipe externally if needed).
func XcodeArchive(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, in XcodeArchiveInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := validateXcodeArchive(in); err != nil {
		return err
	}

	return runXcodeArchive(ctx, ops, w, stderr, in)
}

func validateXcodeArchive(in XcodeArchiveInput) error { //nolint:cyclop // linear checks for required argv, selected identity and optional bounded input file.
	for _, value := range []string{in.Workspace, in.Project, in.Scheme, in.Configuration, in.Destination, in.XcconfigPath, in.BuildNumber} {
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("xcode archive inputs must not contain NUL: %w", errs.ErrUsage)
		}
	}

	if in.Scheme == "" {
		return fmt.Errorf("scheme is required: pass --scheme <name> or set $SCHEME: %w", errs.ErrUsage)
	}

	if in.Configuration == "" {
		return fmt.Errorf("configuration is required: pass --configuration <name> or set $CONFIGURATION: %w", errs.ErrUsage)
	}

	if in.Destination == "" {
		return fmt.Errorf("destination is required: pass --destination <spec> or set $DESTINATION: %w", errs.ErrUsage)
	}

	in.Workspace = strings.TrimSpace(in.Workspace)

	in.Project = strings.TrimSpace(in.Project)
	if (in.Workspace == "") == (in.Project == "") {
		return fmt.Errorf("exactly one workspace or project is required: %w", errs.ErrUsage)
	}

	identity, suffix := in.Project, ".xcodeproj"
	if in.Workspace != "" {
		identity, suffix = in.Workspace, ".xcworkspace"
	}

	if filepath.Ext(identity) != suffix {
		return fmt.Errorf("xcode identity must be a %s directory: %w", suffix, errs.ErrUsage)
	}

	root, err := pathsafe.OpenRoot(identity)
	if err != nil {
		// Classify it. Unwrapped, this reached the CLI as a bare "statat ...
		// no such file or directory" with no sentinel, so the exit code fell
		// through to the generic one and an operator's typo in --project
		// looked like an internal failure rather than a bad input.
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("open Xcode identity %s: %w: %w", identity, err, errs.ErrMissingInput)
		}

		return fmt.Errorf("open Xcode identity %s: %w: %w", identity, err, errs.ErrValidation)
	}

	_ = root.Close()

	if in.XcconfigPath != "" {
		// This is a native-tool filename, not a stdin flag. Retain explicit
		// outside-project paths and symlinks; the reader bounds regular files.
		configPath, pathErr := filepath.Abs(in.XcconfigPath)
		if pathErr != nil {
			return fmt.Errorf("resolve xcconfig path: %w", pathErr)
		}

		if _, readErr := cliio.ReadFile(configPath); readErr != nil {
			return fmt.Errorf("read xcconfig: %w", readErr)
		}
	}

	return nil
}

func runXcodeArchive(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, in XcodeArchiveInput) error { //nolint:varnamelen // writer convention.
	in.Workspace = strings.TrimSpace(in.Workspace)
	in.Project = strings.TrimSpace(in.Project)
	args := []string{"archive"}

	switch {
	case in.Workspace != "":
		args = append(args, "-workspace", in.Workspace)
	case in.Project != "":
		args = append(args, "-project", in.Project)
	}

	args = append(args,
		"-scheme", in.Scheme,
		"-configuration", in.Configuration,
		"-archivePath", "build/app.xcarchive",
		"-destination", in.Destination,
		"-skipPackagePluginValidation",
	)
	if in.XcconfigPath != "" {
		args = append(args, "-xcconfig", in.XcconfigPath)
	}

	if in.BuildNumber != "" {
		args = append(args, "CURRENT_PROJECT_VERSION="+in.BuildNumber)
	}

	if mkdirErr := os.MkdirAll("build", 0o755); mkdirErr != nil { //nolint:gosec // xcodebuild output dir read by archive step.
		return fmt.Errorf("mkdir build: %w", mkdirErr)
	}

	_, _ = fmt.Fprintf(w, "Running: xcodebuild %s\n", strings.Join(args, " "))

	code, err := ops.RunInherit(ctx, w, stderr, args...)
	if err != nil {
		return fmt.Errorf("xcodebuild archive: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("xcodebuild archive exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	return nil
}
