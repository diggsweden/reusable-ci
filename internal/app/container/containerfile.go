// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// ValidateContainerfileInput drives ValidateContainerfile.
type ValidateContainerfileInput struct {
	// Path is the requested containerfile path. When it exists,
	// it's used verbatim; otherwise the directory containing it is
	// searched for Dockerfile* / Containerfile* candidates.
	Path string
}

// ValidateContainerfile resolves a Containerfile path. When Path is an
// existing file, returns it as-is. Otherwise scans Path's directory
// for Dockerfile* / Containerfile* names (non-recursive) and:
//   - errors if no match,
//   - errors if multiple matches (asks for an explicit path),
//   - succeeds with the single match.
//
// On success, emits `containerfile=<path>` via OutputSink.
//
// Mirrors scripts/container/validate-containerfile.sh.
func ValidateContainerfile(ctx context.Context, sink ci.OutputSink, stdout io.Writer, in ValidateContainerfileInput) error {
	if in.Path == "" {
		return fmt.Errorf("Usage: validate-containerfile <containerfile>: %w", errs.ErrUsage)
	}
	if info, err := os.Stat(in.Path); err == nil && !info.IsDir() {
		fmt.Fprintf(stdout, "Using containerfile: %s\n", in.Path)
		return sink.Set(ctx, "containerfile", in.Path)
	}

	dir := filepath.Dir(in.Path)
	matches := findContainerfileCandidates(dir)
	switch len(matches) {
	case 0:
		return fmt.Errorf("Containerfile '%s' not found and no Dockerfile*/Containerfile* match found in '%s'", in.Path, dir)
	case 1:
		fmt.Fprintf(stdout, "Using containerfile: %s\n", matches[0])
		return sink.Set(ctx, "containerfile", matches[0])
	default:
		return fmt.Errorf("Multiple containerfiles found in '%s', please specify an exact path:\n  %s",
			dir, strings.Join(matches, "\n  "))
	}
}

// findContainerfileCandidates scans dir (non-recursive) and returns
// matching files sorted for deterministic test output.
func findContainerfileCandidates(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "Dockerfile") || strings.HasPrefix(name, "Containerfile") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out
}
