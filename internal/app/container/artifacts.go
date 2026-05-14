// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// ValidateArtifactsInput drives ValidateArtifacts.
type ValidateArtifactsInput struct {
	// ProjectType is one of "maven" | "npm" | "gradle".
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
// Mirrors scripts/container/validate-artifacts.sh.
func ValidateArtifacts(stdout, stderr io.Writer, annot output.Annotator, in ValidateArtifactsInput) error {
	if in.ProjectType == "" || in.ArtifactDir == "" {
		return fmt.Errorf("Usage: validate-artifacts <project-type> <artifact-dir> [containerfile-path]: %w", errs.ErrUsage)
	}
	cf := in.ContainerfilePath
	if cf == "" {
		cf = "Containerfile"
	}

	var typeLabel, expectLabel string
	var matcher func(string) bool
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
	default:
		return fmt.Errorf("Unknown project type: %s: %w", in.ProjectType, errs.ErrUsage)
	}

	hits := listMatching(in.ArtifactDir, matcher)
	if len(hits) == 0 {
		annot.Warningf("No %s artifacts found in %s/", typeLabel, in.ArtifactDir)
		fmt.Fprintf(stdout, "Container build may fail if Containerfile expects %s\n", expectLabel)
		fmt.Fprintln(stdout, "This is acceptable if container builds from source instead")
		return nil
	}
	fmt.Fprintf(stdout, "✓ %s artifacts found:\n", typeLabel)
	for _, h := range hits {
		info, _ := os.Stat(h)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Fprintf(stdout, "  %10d  %s\n", size, h)
	}
	checkContainerfileRebuilds(cf, stdout, annot)
	return nil
}

// checkContainerfileRebuilds reads cf and emits an advisory warning
// when it appears to rebuild from source.
func checkContainerfileRebuilds(cf string, stdout io.Writer, annot output.Annotator) {
	body, err := os.ReadFile(cf)
	if err != nil {
		return // file missing → silently skip (matches the bash `[[ -f ... ]]` guard)
	}
	if !container.ContainerfileRebuildsFromSource(string(body)) {
		return
	}
	annot.Warningf("Containerfile rebuilds from source - downloaded artifacts may be ignored")
	fmt.Fprintln(stdout, "This means the container build will NOT use pre-built artifacts, defeating the purpose of separate build steps.")
	fmt.Fprintln(stdout, "Consider updating Containerfile to COPY pre-built artifacts instead of rebuilding.")
	fmt.Fprintf(stdout, "See: %s\n", cf)
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
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if predicate(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}
