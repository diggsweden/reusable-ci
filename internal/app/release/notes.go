// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// PrepareNotesInput drives `release notes`.
type PrepareNotesInput struct {
	SourceFile     string // default "ReleasenotesTmp"
	TargetFile     string // default domain.DefaultReleaseNotesFile
	ReleaseVersion string
	ReleaseCommit  string
}

// PrepareNotes writes the target release-notes file. Source priority:
//
//  1. SourceFile exists and non-empty → copy verbatim.
//  2. ReleaseVersion set → write a fallback "# Release vX.Y.Z" header,
//     plus "Release created from commit ABC" when ReleaseCommit is set.
//  3. otherwise → touch the target file (empty body).
//nolint:cyclop // notes generation: choose source (file/CHANGELOG/git-log) and format.
func PrepareNotes(_ context.Context, out io.Writer, in PrepareNotesInput) error {
	src := in.SourceFile
	if src == "" {
		src = "ReleasenotesTmp"
	}

	tgt := in.TargetFile
	if tgt == "" {
		tgt = domain.DefaultReleaseNotesFile
	}

	if info, err := os.Stat(src); err == nil && !info.IsDir() {
		_, _ = fmt.Fprintf(out, "Changelog artifact found (%d bytes)\n", info.Size())

		data, err := os.ReadFile(src) //nolint:gosec // src is a CLI-flag path.
		if err != nil {
			return fmt.Errorf("read source: %w", err)
		}

		if err := cliio.WriteFile(tgt, data, 0o644); err != nil {
			return fmt.Errorf("write target: %w", err)
		}

		_, _ = fmt.Fprintf(out, "Using git-cliff generated release notes\n")

		return nil
	}

	if in.ReleaseVersion != "" {
		_, _ = fmt.Fprintf(out, "No changelog artifact found - creating fallback\n")

		body := fmt.Sprintf("# Release %s\n\n", in.ReleaseVersion)
		if in.ReleaseCommit != "" {
			body += fmt.Sprintf("Release created from commit %s\n", in.ReleaseCommit)
		}

		return cliio.WriteFile(tgt, []byte(body), 0o644)
	}

	_, _ = fmt.Fprintf(out, "No release notes generated\n")
	// `touch <file>` semantics: create if missing, leave alone otherwise.
	// Skipped when the caller asked for w: there is nothing to touch.
	if tgt == cliio.StdSentinel {
		return nil
	}

	f, err := os.OpenFile(tgt, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // release notes file read by github release step.
	if err != nil {
		return fmt.Errorf("touch target: %w", err)
	}

	return f.Close()
}

// ValidateChangelogInput drives `release verify-changelog`.
type ValidateChangelogInput struct {
	ChangelogFile string
}

// ValidateChangelog verifies the changelog file exists and prints a
// preview. Errors when the file is absent.
func ValidateChangelog(_ context.Context, out io.Writer, in ValidateChangelogInput) error {
	if in.ChangelogFile == "" {
		return fmt.Errorf("changelog file is required: pass --changelog-file <path> or set $CHANGELOG_FILE: %w", errs.ErrUsage)
	}

	info, err := os.Stat(in.ChangelogFile)
	if err != nil {
		return fmt.Errorf("no changelog generated: %w", errs.ErrValidation)
	}

	data, err := os.ReadFile(in.ChangelogFile)
	if err != nil {
		return fmt.Errorf("read changelog: %w", err)
	}

	lineCount := 0

	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}

	_, _ = fmt.Fprintf(out, "✓ Changelog generated successfully: %s\n", in.ChangelogFile)
	_, _ = fmt.Fprintf(out, "  • File size: %d bytes\n", info.Size())
	_, _ = fmt.Fprintf(out, "  • Line count: %d\n\n", lineCount)
	_, _ = fmt.Fprintf(out, "Preview (first 10 lines):\n")
	_, _ = fmt.Fprintf(out, "========================\n")
	_, _ = fmt.Fprintln(out, head(string(data), 10))

	return nil
}

// head returns the first n newline-separated lines (or the whole input
// when fewer). Trailing newline is preserved.
func head(s string, n int) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	count := 0

	for i := range len(s) {
		if s[i] == '\n' {
			count++
			if count == n {
				return s[:i]
			}
		}
	}

	return s
}
