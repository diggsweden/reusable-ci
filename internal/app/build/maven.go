// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package build hosts the toolchain-build use cases (maven / gradle /
// gradle-android / xcode-ios). Toolchain-shaped logic that's pure (e.g.
// snapshot detection, summary rendering) lives in
// internal/domain/build; everything that talks to a CLI binary goes
// through an adapter.
package build

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// MavenOps is the slice of mvn-adapter methods the build use cases need.
// Tests inject a fake; production passes adapter/maven.New().
type MavenOps interface {
	EvalExpression(ctx context.Context, expr string) (string, error)
	RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error
}

// MavenMetadata reads project.{version,groupId,artifactId} via
// `mvn help:evaluate` and writes them to the OutputSink so downstream
// workflow steps can consume them. Mirrors
// scripts/build/extract-maven-metadata.sh end-to-end.
//
// Outputs:
//
//	VERSION       project.version
//	IS_SNAPSHOT   "true" if the version ends with -SNAPSHOT
//	GROUP_ID      project.groupId
//	ARTIFACT_ID   project.artifactId
//
// stdout receives the final "Project: G:A:V" status line.
func MavenMetadata(ctx context.Context, sink ci.OutputSink, ops MavenOps, stdout io.Writer) error {
	version, err := ops.EvalExpression(ctx, "project.version")
	if err != nil {
		return fmt.Errorf("evaluate project.version: %w", err)
	}
	if err := sink.Set(ctx, "VERSION", version); err != nil {
		return fmt.Errorf("set VERSION: %w", err)
	}
	if err := sink.Set(ctx, "IS_SNAPSHOT", strconv.FormatBool(build.IsSnapshot(version))); err != nil {
		return fmt.Errorf("set IS_SNAPSHOT: %w", err)
	}

	groupID, err := ops.EvalExpression(ctx, "project.groupId")
	if err != nil {
		return fmt.Errorf("evaluate project.groupId: %w", err)
	}
	artifactID, err := ops.EvalExpression(ctx, "project.artifactId")
	if err != nil {
		return fmt.Errorf("evaluate project.artifactId: %w", err)
	}
	if err := sink.Set(ctx, "GROUP_ID", groupID); err != nil {
		return fmt.Errorf("set GROUP_ID: %w", err)
	}
	if err := sink.Set(ctx, "ARTIFACT_ID", artifactID); err != nil {
		return fmt.Errorf("set ARTIFACT_ID: %w", err)
	}

	fmt.Fprintf(stdout, "Project: %s:%s:%s\n", groupID, artifactID, version)
	return nil
}

// MavenLibraryInput drives MavenLibrary.
type MavenLibraryInput struct {
	// CLIOpts is the value of $MAVEN_CLI_OPTS, split into argv. The bash
	// uses unquoted expansion to let those flags be word-split; this Go
	// API takes the slice form directly so we never re-implement shell
	// word splitting.
	CLIOpts []string
	// Profile activates a Maven profile (e.g. "central-release"). Empty
	// → no -P flag.
	Profile string
	// SkipTests skips the test phase and propagates -DskipTests=true to
	// the package phase.
	SkipTests bool
}

// MavenLibrary builds a Maven library with sources + javadoc JARs.
// Mirrors scripts/build/maven-library.sh end-to-end. stdout receives
// the human-readable status banner; mvn's own output streams via
// ops.RunInherit.
func MavenLibrary(ctx context.Context, ops MavenOps, stdout, stderr io.Writer, in MavenLibraryInput) error {
	fmt.Fprintf(stdout, "Building Maven library with sources and javadoc...\n")

	profileArg := ""
	if in.Profile != "" {
		profileArg = "-P" + in.Profile
		fmt.Fprintf(stdout, "Using Maven profile: %s\n", in.Profile)
	}

	mvn := func(extra ...string) []string {
		out := make([]string, 0, len(in.CLIOpts)+len(extra)+1)
		out = append(out, in.CLIOpts...)
		out = append(out, extra...)
		if profileArg != "" {
			out = append(out, profileArg)
		}
		return out
	}

	// mvn $MAVEN_CLI_OPTS clean compile $PROFILE_ARG
	if err := ops.RunInherit(ctx, stdout, stderr, mvn("clean", "compile")...); err != nil {
		return fmt.Errorf("mvn clean compile: %w", err)
	}

	if !in.SkipTests {
		fmt.Fprintf(stdout, "Running tests...\n")
		// mvn $MAVEN_CLI_OPTS test $PROFILE_ARG
		if err := ops.RunInherit(ctx, stdout, stderr, mvn("test")...); err != nil {
			return fmt.Errorf("mvn test: %w", err)
		}
	}

	fmt.Fprintf(stdout, "Creating library package with sources and javadoc...\n")
	// mvn $MAVEN_CLI_OPTS package -DskipTests=$SKIP_TESTS $PROFILE_ARG -Dgpg.skip=true
	pkgArgs := mvn("package", "-DskipTests="+strconv.FormatBool(in.SkipTests))
	pkgArgs = append(pkgArgs, "-Dgpg.skip=true")
	if err := ops.RunInherit(ctx, stdout, stderr, pkgArgs...); err != nil {
		return fmt.Errorf("mvn package: %w", err)
	}
	return nil
}
