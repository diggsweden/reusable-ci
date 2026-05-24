// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package publish hosts the publish-side use cases — the pre-flight
// validators and registry-auth checks that run before the workflow
// dispatches `mvn deploy` / `npm publish` / `fastlane` / `bundletool`.
//
// Pure logic (auth-decision tables, error messages) lives in
// internal/domain/publish.
package publish

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// MavenOps is the slice of mvn-adapter methods the publish use cases need.
type MavenOps interface {
	RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) error
}

// MavenCentralDeployInput drives MavenCentralDeploy.
type MavenCentralDeployInput struct {
	// CLIOpts is the value of $MAVEN_CLI_OPTS, split into argv.
	CLIOpts []string
	// SettingsPath is the optional path to a Maven settings.xml. Empty
	// means use the default. Non-empty must exist on disk; the
	// validation runs before the deploy so missing files fail fast with
	// a clear error.
	SettingsPath string
	// Profile is required: the profile activated for the deploy (usually
	// "central-release"). It controls GPG signing and Sonatype staging.
	Profile string
}

// MavenCentralDeploy validates SettingsPath then runs `mvn $CLI_OPTS
// deploy [-s <settings>] -P<profile> -DskipTests`. A missing
// SettingsPath fails as a pre-flight ErrUsage rather than as a
// downstream Maven error, so the publish step surfaces the cause
// immediately.
func MavenCentralDeploy(ctx context.Context, ops MavenOps, w, stderr io.Writer, in MavenCentralDeployInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if strings.TrimSpace(in.Profile) == "" {
		return fmt.Errorf("profile is required: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintln(w, "Deploying to Maven Central...")

	args := make([]string, 0, len(in.CLIOpts)+5)
	args = append(args, in.CLIOpts...)
	args = append(args, "deploy")

	if path := strings.TrimSpace(in.SettingsPath); path != "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("settings file %q: %w: %w", path, err, errs.ErrMissingInput)
		}

		_, _ = fmt.Fprintf(w, "Using settings file: %s\n", path)
		args = append(args, "--settings", path)
	}

	args = append(args, "-P"+in.Profile, "-DskipTests")
	if err := ops.RunInherit(ctx, w, stderr, args...); err != nil {
		return err
	}

	return nil
}

// MavenValidateArtifactsInput drives MavenValidateArtifacts.
type MavenValidateArtifactsInput struct {
	// Root is the directory the search starts from. Empty → cwd. The
	// walker descends into all `target/` subdirectories.
	Root string
}

// MavenValidateArtifactsResult is the (count, paths) summary the use
// case prints. Returned for testability — production callers ignore it.
type MavenValidateArtifactsResult struct {
	SourcesCount int
	JavadocCount int
	JARs         []string // every *.jar under */target/, excluding original-*.jar
}

// MavenValidateArtifacts walks <Root>/**/target/ for the JAR types
// Maven Central requires (sources + javadoc) and errors when either is
// missing.
//nolint:cyclop // checks each required file presence + signature pairing.
func MavenValidateArtifacts(_ context.Context, w, stderr io.Writer, annot output.Annotator, in MavenValidateArtifactsInput) (MavenValidateArtifactsResult, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	root := in.Root
	if root == "" {
		var err error

		root, err = os.Getwd()
		if err != nil {
			return MavenValidateArtifactsResult{}, fmt.Errorf("getwd: %w", err)
		}
	}

	_, _ = fmt.Fprintln(w, "Checking for required artifacts...")

	var res MavenValidateArtifactsResult

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("MavenValidateArtifacts: skipping unreadable entry", "path", path, "err", err)

			return nil
		}

		if d.IsDir() {
			return nil
		}
		// Only files under a `target/` directory anywhere in the tree.
		if !strings.Contains(filepath.ToSlash(path), "/target/") {
			return nil
		}

		name := d.Name()
		switch {
		case strings.HasSuffix(name, "-sources.jar"):
			res.SourcesCount++
			res.JARs = append(res.JARs, path)
		case strings.HasSuffix(name, "-javadoc.jar"):
			res.JavadocCount++
			res.JARs = append(res.JARs, path)
		case strings.HasSuffix(name, ".jar") && !strings.HasPrefix(name, "original-"):
			res.JARs = append(res.JARs, path)
		}

		return nil
	})
	if walkErr != nil {
		return res, fmt.Errorf("walk: %w", walkErr)
	}

	if res.SourcesCount == 0 {
		annot.Errorf("Missing sources JAR. Maven Central requires sources.")
		annot.Errorf("Build with build-type: lib to generate sources and javadoc JARs")

		return res, fmt.Errorf("missing sources JAR: %w", errs.ErrValidation)
	}

	if res.JavadocCount == 0 {
		annot.Errorf("Missing javadoc JAR. Maven Central requires javadoc.")
		annot.Errorf("Build with build-type: lib to generate sources and javadoc JARs")

		return res, fmt.Errorf("missing javadoc JAR: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "✓ All required artifacts present:")
	_, _ = fmt.Fprintf(w, "  - Sources JARs: %d\n", res.SourcesCount)
	_, _ = fmt.Fprintf(w, "  - Javadoc JARs: %d\n", res.JavadocCount)

	for _, jar := range res.JARs {
		_, _ = fmt.Fprintln(w, jar)
	}

	return res, nil
}
