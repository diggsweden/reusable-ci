// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// XcodeVersionInfoInput drives XcodeVersionInfo.
type XcodeVersionInfoInput struct {
	// Project, when non-empty, points at the .xcodeproj directory the
	// version-info should be read from.
	Project string
	// Workspace is accepted for parity with the bash shim but is only
	// used as a fallback marker; in both modes we walk for *.xcodeproj
	// when Project is empty (matches the bash behaviour).
	Workspace string
	// Root is the directory the .xcodeproj search starts from. Empty →
	// cwd. The first match in a depth-first walk wins, like
	// `find . -name "*.xcodeproj" -type d | head -1`.
	Root string
}

// XcodeVersionInfo reads MARKETING_VERSION and CURRENT_PROJECT_VERSION
// from the project.pbxproj inside the resolved .xcodeproj and emits
// them as `version` / `build` outputs. Mirrors
// scripts/apple/get-version-info.sh.
//
// Both default to "unknown" when the project file is missing or the
// keys are absent.
func XcodeVersionInfo(ctx context.Context, sink ci.OutputSink, stderr io.Writer, annot output.Annotator, in XcodeVersionInfoInput) error {
	root := in.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	projectFile := in.Project
	if projectFile == "" {
		projectFile = findFirstXcodeproj(root)
	}

	pbx := filepath.Join(projectFile, "project.pbxproj")
	body, err := os.ReadFile(pbx)
	if err != nil {
		annot.Warningf("Could not determine version from project file")
		if err := sink.Set(ctx, "version", "unknown"); err != nil {
			return err
		}
		return sink.Set(ctx, "build", "unknown")
	}
	got := build.ParseXcodeVersionFromPbxproj(string(body))
	if err := sink.Set(ctx, "version", got.Version); err != nil {
		return err
	}
	if err := sink.Set(ctx, "build", got.Build); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Version: %s (%s)\n", got.Version, got.Build)
	return nil
}

// findFirstXcodeproj returns the first *.xcodeproj directory found
// under root, or empty if none. Matches the bash `find ... | head -1`
// shape with deterministic ordering.
func findFirstXcodeproj(root string) string {
	var first string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("findFirstXcodeproj: skipping unreadable entry", "path", path, "err", err)
			return nil
		}
		if d.IsDir() && strings.HasSuffix(path, ".xcodeproj") {
			first = path
			return filepath.SkipAll
		}
		return nil
	})
	return first
}
