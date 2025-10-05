// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
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
// ValidateContainerfile checks the configured Containerfile exists.
func ValidateContainerfile(ctx context.Context, sink ci.OutputSink, w io.Writer, in ValidateContainerfileInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Path == "" {
		return fmt.Errorf("containerfile path is required: pass --path <file> or set $CONTAINERFILE: %w", errs.ErrUsage)
	}

	if info, err := os.Stat(in.Path); err == nil && !info.IsDir() {
		_, _ = fmt.Fprintf(w, "Using containerfile: %s\n", in.Path)

		return sink.Set(ctx, "containerfile", in.Path)
	}

	dir := filepath.Dir(in.Path)

	matches := findContainerfileCandidates(dir)
	switch len(matches) {
	case 0:
		return fmt.Errorf("containerfile %q not found and no Dockerfile*/Containerfile* match found in %q: %w", in.Path, dir, errs.ErrMissingInput)
	case 1:
		_, _ = fmt.Fprintf(w, "Using containerfile: %s\n", matches[0])

		return sink.Set(ctx, "containerfile", matches[0])
	default:
		return fmt.Errorf("multiple containerfiles found in %q, please specify an exact path:\n  %s: %w",
			dir, strings.Join(matches, "\n  "), errs.ErrValidation)
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
