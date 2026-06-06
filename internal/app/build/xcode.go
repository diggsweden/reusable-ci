// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// XcodeArtifactNameInput drives XcodeArtifactName.
type XcodeArtifactNameInput struct {
	ArtifactName   string
	RepositoryName string
	IncludeTag     bool
	RefName        string
}

// XcodeArtifactName computes and emits the IPA upload-artifact name.
func XcodeArtifactName(ctx context.Context, sink ci.OutputSink, w io.Writer, in XcodeArtifactNameInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	baseName := firstNonEmpty(in.ArtifactName, in.RepositoryName)
	if baseName == "" {
		return fmt.Errorf("artifact name or repository name is required: %w", errs.ErrUsage)
	}

	name := baseName
	if in.IncludeTag && in.RefName != "" {
		name = baseName + "-" + in.RefName
	}

	if err := sink.Set(ctx, "ipa-name", name); err != nil {
		return fmt.Errorf("set ipa-name: %w", err)
	}

	_, _ = fmt.Fprintf(w, "IPA artifact: %s\n", name)

	return nil
}

// XcodeXCConfigInput drives XcodeXCConfig.
type XcodeXCConfigInput struct {
	Base64  string
	TempDir string
}

// XcodeXCConfig decodes an optional xcconfig secret and emits xcconfig-path
// only when a secret was provided.
func XcodeXCConfig(ctx context.Context, sink ci.OutputSink, annot output.Annotator, in XcodeXCConfigInput) error {
	if strings.TrimSpace(in.Base64) == "" {
		annot.Noticef("XCCONFIG_BASE64 secret not set - no xcconfig will be applied")

		return nil
	}

	tempDir := in.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}

	body, err := base64.StdEncoding.DecodeString(stripWhitespace(in.Base64))
	if err != nil {
		return fmt.Errorf("decode xcconfig base64: %w: %w", err, errs.ErrMalformedInput)
	}

	f, err := os.CreateTemp(tempDir, "ci-*.xcconfig") //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		return fmt.Errorf("create xcconfig tempfile: %w", err)
	}

	path := f.Name()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()

		return fmt.Errorf("write xcconfig: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close xcconfig: %w", err)
	}

	if err := sink.Set(ctx, "xcconfig-path", path); err != nil {
		return fmt.Errorf("set xcconfig-path: %w", err)
	}

	annot.Noticef("xcconfig decoded from XCCONFIG_BASE64 secret")

	return nil
}

// XcodeVersionInfoInput drives XcodeVersionInfo.
type XcodeVersionInfoInput struct {
	// Project, when non-empty, points at the .xcodeproj directory the
	// version-info should be read from.
	Project string
	// Workspace is accepted for parity with the bash shim but is only
	// used as a fallback marker; in both modes we walk for *.xcodeproj
	// when Project is empty.
	Workspace string
	// Root is the directory the .xcodeproj search starts from. Empty →
	// cwd. The first match in a depth-first walk wins, like
	// `find . -name "*.xcodeproj" -type d | head -1`.
	Root string
}

// XcodeVersionInfo reads MARKETING_VERSION and CURRENT_PROJECT_VERSION
// from the project.pbxproj inside the resolved .xcodeproj and emits
// them as `version` / `build` outputs. Mirrors
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

	body, err := os.ReadFile(pbx) //nolint:gosec // projectFile is CLI-flag-derived; filename component is hardcoded.
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

	_, _ = fmt.Fprintf(stderr, "Version: %s (%s)\n", got.Version, got.Build)

	return nil
}

// findFirstXcodeproj returns the first *.xcodeproj directory found
// under root, or empty if none. Matches `find ... | head -1`
// shape with deterministic ordering.
func findFirstXcodeproj(root string) string {
	var first string

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("findFirstXcodeproj: skipping unreadable entry", "path", path, "err", err)

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

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}

		return r
	}, value)
}
