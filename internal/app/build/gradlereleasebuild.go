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

// gradleDeps is the collaborator set the gradle build steps share: the summary
// sink they report status to, the gradle tool they drive, and the two writers
// they narrate on. All travel unchanged from the release-build entry down to
// the leaves, so passing them positionally added four parameters to every
// signature and forced a varnamelen waiver (w, stderr) onto each.
//
// summary rides along although the innermost leaf that only runs the SBOM tool
// does not report status, rather than threading a second struct through one
// call. androidSBOMStep reuses this set — Android is a gradle
// variant and already shares this file's helpers (makeGradlewExecutable,
// GradleSBOM, defaultDir).
type gradleDeps struct {
	summary ci.SummarySink
	ops     GradleOps
	w       io.Writer // progress, user-facing
	stderr  io.Writer // underlying tool output
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

	if err := gradleSBOMStep(ctx, gradleDeps{summary: summarySink, ops: ops, w: w, stderr: stderr}, in); err != nil {
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

func gradleSBOMStep(ctx context.Context, deps gradleDeps, in GradleReleaseBuildInput) error {
	outcome := outcomeSkipped

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := gradleBuildSBOM(ctx, deps, in); err != nil {
			outcome = outcomeFailure

			_, _ = fmt.Fprintf(deps.stderr, "WARN: Gradle Build SBOM generation failed (continuing): %v\n", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, deps.summary, appsummary.BuildSBOMStatusInput{
		Ecosystem: "gradle",
		Outcome:   outcome,
		WorkDir:   defaultDir(in.Dir),
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}

func gradleBuildSBOM(ctx context.Context, deps gradleDeps, in GradleReleaseBuildInput) error {
	if in.SBOMToolVersion == "" {
		return fmt.Errorf("SBOM tool version is required (set --sbom-tool-version or $CYCLONEDX_GRADLE_VERSION): %w", errs.ErrUsage)
	}

	return GradleSBOM(ctx, deps.ops, deps.w, deps.stderr, GradleSBOMInput{CycloneDXVersion: in.SBOMToolVersion, WorkingDir: in.Dir})
}
