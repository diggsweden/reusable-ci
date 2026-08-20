// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// GradleOps is the slice of gradle-adapter methods the build use cases
// need. Tests inject a fake; production passes adapter/gradle.New().
type GradleOps interface {
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) error
	RunInDirInherit(ctx context.Context, dir string, w, stderr io.Writer, args ...string) error
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

// GradleMetadataInput drives GradleMetadata.
type GradleMetadataInput struct {
	Dir string
}

// GradleMetadata reads the JVM Gradle convention `version=` from
// gradle.properties and emits version + is-snapshot when present.
// Absence is a warning, not a failure, because some projects compute
// version in build.gradle(.kts) — those get an empty publish summary
// rather than a failed publish.
//
// is-snapshot mirrors the maven metadata leaf so the two publish
// summaries stay symmetrical.
func GradleMetadata(ctx context.Context, sink ci.OutputSink, w io.Writer, annot output.Annotator, in GradleMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	body, err := os.ReadFile(filepath.Join(dir, "gradle.properties")) //nolint:gosec // dir is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		// Best-effort metadata: warn and emit nothing, don't fail the run.
		annot.Warningf("version not found in gradle.properties")

		return nil //nolint:nilerr // best-effort: warning already emitted
	}

	version := gradleProperty(string(body), "version")
	if version == "" {
		annot.Warningf("version not found in gradle.properties")

		return nil
	}

	if err := sink.Set(ctx, "version", version); err != nil {
		return fmt.Errorf("set version: %w", err)
	}

	// SetBool rather than Set so JSON consumers get a real boolean,
	// matching the maven leaf.
	if err := sink.SetBool(ctx, "is-snapshot", build.IsSnapshot(version)); err != nil {
		return fmt.Errorf("set is-snapshot: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Version: %s\n", version)

	return nil
}

// GradleApplicationInput drives GradleApplication.
type GradleApplicationInput struct {
	// Tasks is the gradle task list, space-separated. The workflow passes
	// $GRADLE_TASKS verbatim. Whitespace-separated tokens become separate argv
	// entries — no shell re-splitting.
	Tasks string
	// SkipTests appends `-x test` to the gradle invocation.
	SkipTests bool
}

// GradleApplication runs `./gradlew <tasks>` in the current working
// directory, appending `-x test` when SkipTests is true. At least one
// task is required; an empty tasks string fails with ErrUsage.
func GradleApplication(ctx context.Context, ops GradleOps, w, stderr io.Writer, in GradleApplicationInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	tasks := strings.Fields(in.Tasks)
	if len(tasks) == 0 {
		return fmt.Errorf("at least one gradle task is required: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(w, "Running Gradle tasks: %s\n", strings.Join(tasks, " "))

	args := tasks
	if in.SkipTests {
		args = append(args, "-x", "test")
	}

	// Adapter already labels its error with the binary + first task;
	// wrapping with the full task list here would double-prefix.
	return ops.RunInherit(ctx, w, stderr, args...)
}

// GradleSBOM applies the cyclonedx-gradle-plugin via a throwaway init
// script and runs `cyclonedxBom` to produce build/reports/bom.json.
// This is the Gradle build-SBOM implementation behind `reusable-ci build gradle sbom`.
//
// The init script is written to a tempfile that is deleted before
// return regardless of success.
//
//nolint:cyclop // executes 4 gradle phases each gated on detected config.
func GradleSBOM(ctx context.Context, ops GradleOps, w, stderr io.Writer, in GradleSBOMInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.CycloneDXVersion == "" {
		return fmt.Errorf("cyclonedx-gradle version is required: pass --cyclonedx-version <ver> or set $CYCLONEDX_GRADLE_VERSION: %w", errs.ErrUsage)
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
		return fmt.Errorf("gradlew not found or not a file in %s: %w", wd, errs.ErrMissingInput)
	}
	// 0o111 = any execute bit set (owner/group/other).
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("gradlew not executable in %s: %w", wd, errs.ErrValidation)
	}

	f, err := os.CreateTemp("", "*.init.gradle.kts") //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	// Adapter labels its error with the first non-flag arg
	// ("cyclonedxBom"); wrapping again here would double-prefix and
	// shadow the more useful adapter label.
	return ops.RunInDirInherit(ctx, wd, w, stderr, "--init-script", initPath, "cyclonedxBom")
}

func gradleProperty(body, key string) string {
	prefix := key + "="

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}

	return ""
}
