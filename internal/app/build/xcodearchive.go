// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
//nolint:cyclop // xcodebuild flow: workspace/project dispatch + scheme/config inference + archive.
func XcodeArchive(ctx context.Context, ops XcodeBuildOps, w, stderr io.Writer, in XcodeArchiveInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Scheme == "" {
		return fmt.Errorf("scheme is required: pass --scheme <name> or set $SCHEME: %w", errs.ErrUsage)
	}

	if in.Configuration == "" {
		return fmt.Errorf("configuration is required: pass --configuration <name> or set $CONFIGURATION: %w", errs.ErrUsage)
	}

	if in.Destination == "" {
		return fmt.Errorf("destination is required: pass --destination <spec> or set $DESTINATION: %w", errs.ErrUsage)
	}

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

	if err := os.MkdirAll("build", 0o755); err != nil { //nolint:gosec // xcodebuild output dir read by archive step.
		return fmt.Errorf("mkdir build: %w", err)
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
