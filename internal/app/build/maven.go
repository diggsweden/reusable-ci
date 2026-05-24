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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// MavenOps is the slice of mvn-adapter methods the build use cases need.
// Tests inject a fake; production passes adapter/maven.New().
//
// EvalExpression remains here as the fallback for POMs that use property
// interpolation (e.g. <version>${revision}</version>) — about 5% of real
// projects. The common path reads pom.xml directly.
type MavenOps interface {
	EvalExpression(ctx context.Context, expr string) (string, error)
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) error
}

// MavenMetadataInput drives MavenMetadata.
type MavenMetadataInput struct {
	// Dir is the project root containing pom.xml. Defaults to ".".
	Dir string
}

// MavenMetadata reads project.{version,groupId,artifactId} from pom.xml
// and writes them to the OutputSink so downstream workflow steps can
// consume them.
//
// Performance: this used to fork three `mvn help:evaluate` subprocesses
// (~3-5s each, cold JVM = 9-15s total). pom.xml parsing is ~10ms.
//
// Fallback: when a field carries an unresolved Maven property reference
// (${name}), the value is resolved via `mvn help:evaluate` for that
// single expression. ArtifactID is rarely property-resolved; Version is
// the common case (CI/CD pipelines with -Drevision=…).
//
// Outputs:
//
//	version       project.version (or inherited from <parent>)
//	is-snapshot   "true" if the version ends with -SNAPSHOT
//	group-id      project.groupId (or inherited from <parent>)
//	artifact-id   project.artifactId
//
// w receives the final "Project: G:A:V" status line.
//nolint:cyclop // POM-read flow: read manifest → resolve fields → validate → emit (one branch per field).
func MavenMetadata(ctx context.Context, sink ci.OutputSink, ops MavenOps, w io.Writer, in MavenMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	pomPath := filepath.Join(dir, "pom.xml")

	body, err := os.ReadFile(pomPath) //nolint:gosec // dir is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("pom.xml not found at %q: %w", pomPath, errs.ErrMissingInput)
		}

		return fmt.Errorf("read pom.xml at %q: %w", pomPath, err)
	}

	pom, err := build.ParsePOM(body)
	if err != nil {
		return err
	}

	version, err := resolvePOMField(ctx, ops, pom.Version, "project.version")
	if err != nil {
		return err
	}

	groupID, err := resolvePOMField(ctx, ops, pom.GroupID, "project.groupId")
	if err != nil {
		return err
	}

	artifactID, err := resolvePOMField(ctx, ops, pom.ArtifactID, "project.artifactId")
	if err != nil {
		return err
	}

	// Strings flow through Set; the snapshot signal is a typed bool so
	// JSON consumers get `"is-snapshot": false` instead of the string
	// "false". Order is deterministic — map iteration would randomise.
	stringOutputs := []struct{ key, value string }{
		{"version", version}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"group-id", groupID},
		{"artifact-id", artifactID},
	}
	for _, kv := range stringOutputs {
		if err := sink.Set(ctx, kv.key, kv.value); err != nil {
			return fmt.Errorf("set %s: %w", kv.key, err)
		}
	}

	if err := sink.SetBool(ctx, "is-snapshot", build.IsSnapshot(version)); err != nil {
		return fmt.Errorf("set is-snapshot: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Project: %s:%s:%s\n", groupID, artifactID, version)

	return nil
}

// resolvePOMField returns the literal value when it does not carry a
// property reference, or asks Maven to expand it when it does. Errors
// from the parse path are propagated; mvn failures are wrapped with the
// field name so callers see which property failed.
func resolvePOMField(ctx context.Context, ops MavenOps, value, mvnExpr string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s missing from pom.xml: %w", mvnExpr, errs.ErrMissingInput)
	}

	if !build.POMHasUnresolvedProperty(value) {
		return value, nil
	}

	if ops == nil {
		return "", fmt.Errorf("%s requires property expansion but no mvn adapter available: %w", mvnExpr, errs.ErrUsage)
	}

	resolved, err := ops.EvalExpression(ctx, mvnExpr)
	if err != nil {
		return "", fmt.Errorf("expand %s via mvn help:evaluate: %w", mvnExpr, err)
	}

	return resolved, nil
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

// MavenApplicationInput drives MavenApplication.
type MavenApplicationInput struct {
	// CLIOpts is the value of $MAVEN_CLI_OPTS, split into argv (no shell
	// re-splitting needed; the workflow forwards the value verbatim).
	CLIOpts []string
	// SkipTests propagates -DskipTests to `mvn clean package`.
	SkipTests bool
}

// MavenApplication builds a Maven application: `mvn $CLI_OPTS clean package
// [-DskipTests]`. Subsumes the SKIP_TESTS branching in build-maven.yml so
// the decision lives in Go, not shell.
func MavenApplication(ctx context.Context, ops MavenOps, w, stderr io.Writer, in MavenApplicationInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintln(w, "Building Maven application...")

	args := make([]string, 0, len(in.CLIOpts)+3)
	args = append(args, in.CLIOpts...)

	args = append(args, "clean", "package")
	if in.SkipTests {
		args = append(args, "-DskipTests")
	}

	// The mvn adapter already labels its error with the subcommand
	// ("mvn clean: …"); wrapping again here would double-prefix.
	return ops.RunInherit(ctx, w, stderr, args...)
}

// MavenLibrary builds a Maven library with sources + javadoc JARs.
// w receives the human-readable status banner; mvn's own output
// streams via ops.RunInherit.
func MavenLibrary(ctx context.Context, ops MavenOps, w, stderr io.Writer, in MavenLibraryInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(w, "Building Maven library with sources and javadoc...\n")

	profileArg := ""
	if in.Profile != "" {
		profileArg = "-P" + in.Profile
		_, _ = fmt.Fprintf(w, "Using Maven profile: %s\n", in.Profile)
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
	if err := ops.RunInherit(ctx, w, stderr, mvn("clean", "compile")...); err != nil {
		return err
	}

	if !in.SkipTests {
		_, _ = fmt.Fprintf(w, "Running tests...\n")
		// mvn $MAVEN_CLI_OPTS test $PROFILE_ARG
		if err := ops.RunInherit(ctx, w, stderr, mvn("test")...); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(w, "Creating library package with sources and javadoc...\n")
	// mvn $MAVEN_CLI_OPTS package -DskipTests=$SKIP_TESTS $PROFILE_ARG -Dgpg.skip=true
	pkgArgs := mvn("package", "-DskipTests="+strconv.FormatBool(in.SkipTests))

	pkgArgs = append(pkgArgs, "-Dgpg.skip=true")
	if err := ops.RunInherit(ctx, w, stderr, pkgArgs...); err != nil {
		return err
	}

	return nil
}
