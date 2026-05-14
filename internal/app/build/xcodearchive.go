// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error)
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
// flag composition. Mirrors scripts/apple/archive-app.sh.
//
// The bash piped to `xcbeautify`; the Go port streams xcodebuild's
// own output through (callers can pipe externally if needed).
func XcodeArchive(ctx context.Context, ops XcodeBuildOps, stdout, stderr io.Writer, in XcodeArchiveInput) error {
	if in.Scheme == "" {
		return fmt.Errorf("SCHEME is required: %w", errs.ErrUsage)
	}
	if in.Configuration == "" {
		return fmt.Errorf("CONFIGURATION is required: %w", errs.ErrUsage)
	}
	if in.Destination == "" {
		return fmt.Errorf("DESTINATION is required: %w", errs.ErrUsage)
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
	if err := os.MkdirAll("build", 0o755); err != nil {
		return fmt.Errorf("mkdir build: %w", err)
	}
	fmt.Fprintf(stdout, "Running: xcodebuild %s\n", strings.Join(args, " "))
	code, err := ops.RunInherit(ctx, stdout, stderr, args...)
	if err != nil {
		return fmt.Errorf("xcodebuild archive: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("xcodebuild archive exited with status %d", code)
	}
	return nil
}
