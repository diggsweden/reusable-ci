// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// GradleOps is the slice of gradle-adapter methods the build use cases
// need. Tests inject a fake; production passes adapter/gradle.New().
type GradleOps interface {
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error
}

// GradleSBOMInput drives GradleSBOM.
type GradleSBOMInput struct {
	// CycloneDXVersion is the cyclonedx-gradle-plugin version
	// (Renovate-managed in the calling workflow).
	CycloneDXVersion string
	// WorkingDir is the project root containing ./gradlew. Used to
	// pre-flight-check for the wrapper. Empty → current directory.
	WorkingDir string
}

// GradleSBOM applies the cyclonedx-gradle-plugin via a throwaway init
// script and runs `cyclonedxBom` to produce build/reports/bom.json.
// Mirrors scripts/sbom/generate-gradle-sbom.sh end-to-end.
//
// The init script is written to a tempfile that is deleted before
// return regardless of success.
func GradleSBOM(ctx context.Context, ops GradleOps, stdout, stderr io.Writer, in GradleSBOMInput) error {
	if in.CycloneDXVersion == "" {
		return fmt.Errorf("CYCLONEDX_GRADLE_VERSION is required: %w", errs.ErrUsage)
	}

	wd := in.WorkingDir
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	wrapper := filepath.Join(wd, "gradlew")
	info, err := os.Stat(wrapper)
	if err != nil || info.IsDir() {
		return fmt.Errorf("gradlew not found or not a file in %s", wd)
	}
	// 0o111 = any execute bit set (owner/group/other).
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("gradlew not executable in %s", wd)
	}

	f, err := os.CreateTemp("", "*.init.gradle.kts")
	if err != nil {
		return fmt.Errorf("create init script tempfile: %w", err)
	}
	initPath := f.Name()
	defer func() { _ = os.Remove(initPath) }()

	if _, err := io.WriteString(f, build.RenderGradleInitScript(in.CycloneDXVersion)); err != nil {
		_ = f.Close()
		return fmt.Errorf("write init script: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close init script: %w", err)
	}

	if err := ops.RunInherit(ctx, stdout, stderr, "--init-script", initPath, "cyclonedxBom"); err != nil {
		return fmt.Errorf("./gradlew cyclonedxBom: %w", err)
	}
	return nil
}
