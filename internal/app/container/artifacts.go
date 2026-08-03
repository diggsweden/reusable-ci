// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// ValidateArtifactsInput drives ValidateArtifacts.
type ValidateArtifactsInput struct {
	// ProjectType is one of "maven" | "npm" | "gradle" | "go" | "cargo".
	ProjectType string
	// ArtifactDir is the directory holding the downloaded artifacts.
	ArtifactDir string
	// ContainerfilePath is checked for COPY-instead-of-rebuild
	// advisory warnings. Empty → "Containerfile".
	ContainerfilePath string
}

// ValidateArtifacts checks that the expected artifact files for a
// project type are present in ArtifactDir, and warns when the
// Containerfile appears to rebuild from source (which would defeat
// the purpose of separate build steps).
//
//nolint:cyclop // validates each presence/exclusivity rule independently.
func ValidateArtifacts(w, stderr io.Writer, annot output.Annotator, in ValidateArtifactsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.ProjectType == "" || in.ArtifactDir == "" {
		return fmt.Errorf("usage: validate-artifacts <project-type> <artifact-dir> [containerfile-path]: %w", errs.ErrUsage)
	}

	cf := in.ContainerfilePath
	if cf == "" {
		cf = "Containerfile"
	}

	var (
		typeLabel, expectLabel string
		matcher                func(string) bool
	)

	switch projecttype.Type(in.ProjectType) {
	case projecttype.Maven:
		typeLabel, expectLabel = "Maven", "JAR files"
		matcher = func(name string) bool { return strings.HasSuffix(name, ".jar") }
	case projecttype.NPM:
		typeLabel, expectLabel = "NPM", "built files"
		matcher = func(string) bool { return true } // bash globs "*"
	case projecttype.Gradle:
		typeLabel, expectLabel = "Gradle", "JAR files"
		matcher = func(name string) bool { return strings.HasSuffix(name, ".jar") }
	case projecttype.Go:
		typeLabel, expectLabel = "Go", "binaries"
		matcher = func(string) bool { return true }
	case projecttype.Cargo:
		typeLabel, expectLabel = "Cargo", "binaries"
		matcher = func(string) bool { return true }
	default:
		return fmt.Errorf("unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}

	// Go + Cargo land binaries at dist/<goos>-<goarch>/<binary>-<goos>-<goarch>
	// (nested), so recurse. JVM/NPM artifacts sit at the top level of the
	// matrix-uploaded directory.
	var hits []string

	pt := projecttype.Type(in.ProjectType)
	if pt == projecttype.Go || pt == projecttype.Cargo {
		hits = listMatchingRecursive(in.ArtifactDir, matcher)
	} else {
		hits = listMatching(in.ArtifactDir, matcher)
	}

	if len(hits) == 0 {
		annot.Errorf("No %s artifacts found in %s/", typeLabel, in.ArtifactDir)

		_, _ = fmt.Fprintf(w, "Expected %s in %s/ — the upstream build job should have uploaded them.\n", expectLabel, in.ArtifactDir)
		_, _ = fmt.Fprintln(w, "If this container does not actually depend on this ecosystem's artifacts,")
		_, _ = fmt.Fprintln(w, "drop the matching `from:` entry in artifacts.yml so the verify step is skipped.")

		return fmt.Errorf("no %s artifacts in %s: %w", typeLabel, in.ArtifactDir, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "%s %s artifacts found:\n", clicolor.Check(w), typeLabel)

	for _, h := range hits { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		info, _ := os.Stat(h)

		size := int64(0)
		if info != nil {
			size = info.Size()
		}

		_, _ = fmt.Fprintf(w, "  %10d  %s\n", size, h)
	}

	checkContainerfileRebuilds(cf, w, annot)

	return nil
}

// checkContainerfileRebuilds reads cf and emits an advisory warning
// when it appears to rebuild from source.
func checkContainerfileRebuilds(cf string, w io.Writer, annot output.Annotator) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := os.ReadFile(cf) //nolint:gosec // cf is a CLI-flag path.
	if err != nil {
		return // file missing → silently skip
	}

	if !container.ContainerfileRebuildsFromSource(string(body)) {
		return
	}

	annot.Warningf("Containerfile rebuilds from source - downloaded artifacts may be ignored")

	_, _ = fmt.Fprintln(w, "This means the container build will NOT use pre-built artifacts, defeating the purpose of separate build steps.")
	_, _ = fmt.Fprintln(w, "Consider updating Containerfile to COPY pre-built artifacts instead of rebuilding.")
	_, _ = fmt.Fprintf(w, "See: %s\n", cf)
}

// listMatching returns top-level files in dir matched by predicate
// (non-recursive). Returns nil when dir doesn't exist.
func listMatching(dir string, predicate func(name string) bool) []string {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []string

	for _, e := range entries { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if e.IsDir() {
			continue
		}

		if predicate(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}

	return out
}

func listMatchingRecursive(dir string, predicate func(name string) bool) []string {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var out []string

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		// Skip per-entry errors (permissions, race-deleted files) and
		// keep walking — the function returns the entries we could read.
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entry, keep walking
		}

		if predicate(d.Name()) {
			out = append(out, path)
		}

		return nil
	})

	return out
}
