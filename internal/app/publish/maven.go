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
// missing. Mirrors scripts/publish/maven-validate-artifacts.sh.
func MavenValidateArtifacts(_ context.Context, stdout, stderr io.Writer, annot output.Annotator, in MavenValidateArtifactsInput) (MavenValidateArtifactsResult, error) {
	root := in.Root
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return MavenValidateArtifactsResult{}, fmt.Errorf("getwd: %w", err)
		}
	}

	fmt.Fprintln(stdout, "Checking for required artifacts...")

	var res MavenValidateArtifactsResult
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("MavenValidateArtifacts: skipping unreadable entry", "path", path, "err", err)
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

	fmt.Fprintln(stdout, "✓ All required artifacts present:")
	fmt.Fprintf(stdout, "  - Sources JARs: %d\n", res.SourcesCount)
	fmt.Fprintf(stdout, "  - Javadoc JARs: %d\n", res.JavadocCount)
	for _, jar := range res.JARs {
		fmt.Fprintln(stdout, jar)
	}
	return res, nil
}
