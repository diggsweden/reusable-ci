// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// GradleReleaseBuildInput drives GradleReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the Gradle-specific knobs.
type GradleReleaseBuildInput struct {
	ReleaseBuildOptions

	// Tasks is the gradle task list ($GRADLE_TASKS), space-separated.
	Tasks string
	// SBOMToolVersion pins the cyclonedx-gradle-plugin; required when
	// EnableBuildSBOM. Renovate-managed in the calling workflow.
	SBOMToolVersion string
	// JavaVersion is reported in the build summary only.
	JavaVersion string
}

// GradleReleaseBuild runs the whole Gradle release build as one step: make the
// wrapper executable, resolve metadata, run the gradle tasks, generate the Build
// SBOM (unless disabled), and write the SBOM-status + build summaries.
//
// It is the Gradle sibling of GoReleaseBuild — the binary-owned build sequence
// (Design Rule 1). ./gradlew runs in the process working directory (the forge
// job's working-directory). The SBOM is best-effort (failure warns + reports
// failure status but does not fail the build).
//
//nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
func GradleReleaseBuild(ctx context.Context, summarySink ci.SummarySink, ops GradleOps, w, stderr io.Writer, in GradleReleaseBuildInput) error {
	if err := makeGradlewExecutable(in.Dir); err != nil {
		return err
	}

	version := resolveGradleVersion(in.Dir)
	_, _ = fmt.Fprintf(w, "Gradle release build: version=%s tasks=%s\n", version, in.Tasks)

	if err := GradleApplication(ctx, ops, w, stderr, GradleApplicationInput{Tasks: in.Tasks, SkipTests: in.SkipTests}); err != nil {
		return fmt.Errorf("gradle build: %w", err)
	}

	if err := gradleSBOMStep(ctx, summarySink, ops, in, w, stderr); err != nil {
		return err
	}

	if err := appsummary.GradleBuild(ctx, summarySink, appsummary.GradleBuildInput{
		JavaVersion: in.JavaVersion,
		GradleTasks: in.Tasks,
		SkipTests:   in.SkipTests,
		Version:     version,
	}); err != nil {
		return fmt.Errorf("write build summary: %w", err)
	}

	return nil
}

// makeGradlewExecutable mirrors the workflow's `chmod +x ./gradlew`: the wrapper
// is committed without the execute bit on some checkouts.
func makeGradlewExecutable(dir string) error {
	if dir == "" {
		dir = "."
	}

	wrapper := filepath.Join(dir, "gradlew")
	if err := os.Chmod(wrapper, 0o755); err != nil { //nolint:gosec // the gradle wrapper must be executable to run the build.
		return fmt.Errorf("make %q executable: %w", wrapper, err)
	}

	return nil
}

func gradleSBOMStep(ctx context.Context, summarySink ci.SummarySink, ops GradleOps, in GradleReleaseBuildInput, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names — testing/http/io conventions.
	outcome := outcomeSkipped

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := gradleBuildSBOM(ctx, ops, in, w, stderr); err != nil {
			outcome = outcomeFailure

			_, _ = fmt.Fprintf(stderr, "WARN: Gradle Build SBOM generation failed (continuing): %v\n", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, summarySink, appsummary.BuildSBOMStatusInput{
		Ecosystem: "gradle",
		Outcome:   outcome,
		WorkDir:   defaultDir(in.Dir),
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}

func gradleBuildSBOM(ctx context.Context, ops GradleOps, in GradleReleaseBuildInput, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names — testing/http/io conventions.
	if in.SBOMToolVersion == "" {
		return fmt.Errorf("SBOM tool version is required (set --sbom-tool-version or $CYCLONEDX_GRADLE_VERSION): %w", errs.ErrUsage)
	}

	return GradleSBOM(ctx, ops, w, stderr, GradleSBOMInput{CycloneDXVersion: in.SBOMToolVersion, WorkingDir: in.Dir})
}
