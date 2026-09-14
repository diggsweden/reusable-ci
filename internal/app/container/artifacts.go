// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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
func ValidateArtifacts(w io.Writer, annot output.Annotator, in ValidateArtifactsInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	root, rootErr := pathsafe.OpenRoot(in.ArtifactDir)
	if errors.Is(rootErr, fs.ErrNotExist) {
		return fmt.Errorf("open artifact directory: %w: %w", rootErr, errs.ErrMissingInput)
	}

	if rootErr != nil {
		return rootErr
	}

	if root != nil {
		defer func() { _ = root.Close() }()
	}

	// Go + Cargo land binaries at dist/<goos>-<goarch>/<binary>-<goos>-<goarch>
	// (nested), so recurse. JVM/NPM artifacts sit at the top level of the
	// matrix-uploaded directory.
	var hits []string

	pt := projecttype.Type(in.ProjectType)

	if root != nil {
		var err error

		hits, err = listMatching(root, in.ArtifactDir, pt == projecttype.Go || pt == projecttype.Cargo, matcher)
		if err != nil {
			return err
		}
	}

	if len(hits) == 0 {
		annot.Errorf("No %s artifacts found in %s/", typeLabel, in.ArtifactDir)

		_, _ = fmt.Fprintf(w, "Expected %s in %s/ — the upstream build job should have uploaded them.\n", expectLabel, in.ArtifactDir)
		_, _ = fmt.Fprintln(w, "If this container does not actually depend on this ecosystem's artifacts,")
		_, _ = fmt.Fprintln(w, "drop the matching `from:` entry in artifacts.yml so the verify step is skipped.")

		return fmt.Errorf("no %s artifacts in %s: %w", typeLabel, in.ArtifactDir, errs.ErrValidation)
	}

	sizes := make([]int64, len(hits))
	for index, hit := range hits {
		rel, err := filepath.Rel(in.ArtifactDir, hit)
		if err != nil {
			return err
		}

		info, err := root.Lstat(rel)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("artifact must be a confined regular file: %s: %w", hit, errs.ErrValidation)
		}

		sizes[index] = info.Size()
	}

	_, _ = fmt.Fprintf(w, "%s %s artifacts found:\n", clicolor.Check(w), typeLabel)

	var largest int64

	for index, h := range hits { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		size := sizes[index]

		largest = max(largest, size)

		_, _ = fmt.Fprintf(w, "  %10d  %s\n", size, h)
	}

	// Every match is zero bytes. This verb exists so a container is never
	// built around a missing artifact, and a truncated upload satisfies
	// "a file is present" while shipping exactly the empty image the check
	// is meant to prevent — the size was already being read and printed
	// here, and passed anyway. Kept separate from the no-matches branch
	// above: "nothing was uploaded" and "what was uploaded is empty" send
	// the operator to different places.
	if largest == 0 {
		annot.Errorf("All %s artifacts in %s/ are empty (0 bytes)", typeLabel, in.ArtifactDir)

		_, _ = fmt.Fprintf(w, "Expected %s with content — the upstream build job uploaded %d empty file(s).\n", expectLabel, len(hits))

		return fmt.Errorf("all %s artifacts in %s are empty: %w", typeLabel, in.ArtifactDir, errs.ErrValidation)
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

// listMatching walks the checked directory handle without following links.
func listMatching(root *os.Root, dir string, recursive bool, predicate func(string) bool) ([]string, error) {
	var out []string

	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if path != "." && !recursive {
				return fs.SkipDir
			}

			return nil
		}

		if predicate(entry.Name()) {
			out = append(out, filepath.Join(dir, path))
		}

		return nil
	})

	return out, err
}
