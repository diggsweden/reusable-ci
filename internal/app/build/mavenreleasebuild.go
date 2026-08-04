// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Maven build-type values.
const (
	mavenBuildTypeApp = "app"
	mavenBuildTypeLib = "lib"
)

// MavenReleaseBuildInput drives MavenReleaseBuild. It embeds the shared
// ReleaseBuildOptions and adds the Maven-specific knobs as configuration: build
// type, CLI opts, profile, and the pinned cyclonedx plugin version.
type MavenReleaseBuildInput struct {
	ReleaseBuildOptions

	// BuildType selects the build shape: "app" (clean package) or "lib"
	// (compile/test/package with sources + javadoc).
	BuildType string
	// CLIOpts is $MAVEN_CLI_OPTS already split into argv.
	CLIOpts []string
	// Profile activates a Maven profile (library builds).
	Profile string
	// SBOMToolVersion pins the cyclonedx-maven-plugin; required when
	// EnableBuildSBOM. Renovate-managed in the calling workflow.
	SBOMToolVersion string
	// JavaVersion is reported in the build summary only.
	JavaVersion string
}

// MavenInstall installs the project (and its modules) to the local repository
// so multi-module builds resolve sibling modules: `mvn $CLI_OPTS install
// -DskipTests`.
func MavenInstall(ctx context.Context, ops MavenOps, cliOpts []string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names (w) — testing/http/io conventions.
	args := make([]string, 0, len(cliOpts)+2)
	args = append(args, cliOpts...)
	args = append(args, "install", "-DskipTests")

	return ops.RunInherit(ctx, w, stderr, args...)
}

// MavenBuildSBOM generates an aggregate Build SBOM via the pinned
// cyclonedx-maven-plugin.
func MavenBuildSBOM(ctx context.Context, ops MavenOps, cliOpts []string, toolVersion string, w, stderr io.Writer) error { //nolint:varnamelen // idiomatic short names (w) — testing/http/io conventions.
	if toolVersion == "" {
		return fmt.Errorf("SBOM tool version is required (set --sbom-tool-version or $CYCLONEDX_MAVEN_VERSION): %w", errs.ErrUsage)
	}

	args := make([]string, 0, len(cliOpts)+1)
	args = append(args, cliOpts...)
	args = append(args, "org.cyclonedx:cyclonedx-maven-plugin:"+toolVersion+":makeAggregateBom")

	return ops.RunInherit(ctx, w, stderr, args...)
}

// mavenDeps is the collaborator set the maven build steps share: the summary
// sink they report status to, the maven tool they drive, and the two writers
// they narrate on. All travel unchanged from MavenReleaseBuild down to the
// leaves, so passing them positionally added four parameters to every
// signature and forced a varnamelen waiver (w, stderr) onto each.
//
// summary rides along although mavenBuildArtifact does not report status,
// rather than threading a second struct through one call.
type mavenDeps struct {
	summary ci.SummarySink
	ops     MavenOps
	w       io.Writer // progress, user-facing
	stderr  io.Writer // underlying tool output
}

// MavenReleaseBuild runs the whole Maven release build as one step: install
// modules, resolve metadata, build the app or library, generate the Build SBOM
// (unless disabled), and write the SBOM-status + build summaries.
//
// It is the Maven sibling of GoReleaseBuild — the binary-owned build sequence
// (Design Rule 1). mvn runs in the process working directory (the forge job's
// working-directory), matching MavenOps. The SBOM is best-effort (failure warns
// + reports failure status but does not fail the build).
//
//nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
func MavenReleaseBuild(ctx context.Context, summarySink ci.SummarySink, ops MavenOps, w, stderr io.Writer, in MavenReleaseBuildInput) error {
	if in.BuildType != mavenBuildTypeApp && in.BuildType != mavenBuildTypeLib {
		return fmt.Errorf("build-type must be %q or %q, got %q: %w", mavenBuildTypeApp, mavenBuildTypeLib, in.BuildType, errs.ErrUsage)
	}

	if err := MavenInstall(ctx, ops, in.CLIOpts, w, stderr); err != nil {
		return fmt.Errorf("mvn install: %w", err)
	}

	meta, err := resolveMavenMetadata(ctx, ops, MavenMetadataInput{Dir: in.Dir})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "Maven release build (%s): %s:%s:%s\n", in.BuildType, meta.groupID, meta.artifactID, meta.version)

	deps := mavenDeps{summary: summarySink, ops: ops, w: w, stderr: stderr}

	if err := mavenBuildArtifact(ctx, deps, in); err != nil {
		return err
	}

	if err := mavenSBOMStep(ctx, deps, in); err != nil {
		return err
	}

	if err := appsummary.MavenBuild(ctx, summarySink, appsummary.MavenBuildInput{
		BuildType:   in.BuildType,
		GroupID:     meta.groupID,
		ArtifactID:  meta.artifactID,
		Version:     meta.version,
		JavaVersion: in.JavaVersion,
		SkipTests:   in.SkipTests,
		IsSnapshot:  build.IsSnapshot(meta.version),
	}); err != nil {
		return fmt.Errorf("write build summary: %w", err)
	}

	return nil
}

func mavenBuildArtifact(ctx context.Context, deps mavenDeps, in MavenReleaseBuildInput) error {
	switch in.BuildType {
	case mavenBuildTypeApp:
		if err := MavenApplication(ctx, deps.ops, deps.w, deps.stderr, MavenApplicationInput{CLIOpts: in.CLIOpts, SkipTests: in.SkipTests}); err != nil {
			return fmt.Errorf("mvn application build: %w", err)
		}
	case mavenBuildTypeLib:
		if err := MavenLibrary(ctx, deps.ops, deps.w, deps.stderr, MavenLibraryInput{CLIOpts: in.CLIOpts, Profile: in.Profile, SkipTests: in.SkipTests}); err != nil {
			return fmt.Errorf("mvn library build: %w", err)
		}
	}

	return nil
}

func mavenSBOMStep(ctx context.Context, deps mavenDeps, in MavenReleaseBuildInput) error {
	outcome := outcomeSkipped

	if in.EnableBuildSBOM {
		outcome = outcomeSuccess
		if err := MavenBuildSBOM(ctx, deps.ops, in.CLIOpts, in.SBOMToolVersion, deps.w, deps.stderr); err != nil {
			outcome = outcomeFailure

			_, _ = fmt.Fprintf(deps.stderr, "WARN: Maven Build SBOM generation failed (continuing): %v\n", err)
		}
	}

	if err := appsummary.BuildSBOMStatus(ctx, deps.summary, appsummary.BuildSBOMStatusInput{
		Ecosystem: "maven",
		Outcome:   outcome,
		WorkDir:   defaultDir(in.Dir),
	}); err != nil {
		return fmt.Errorf("write SBOM status: %w", err)
	}

	return nil
}
