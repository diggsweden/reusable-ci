// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// SwiftFormatOps is the swift-format adapter surface needed by the lint
// use case.
type SwiftFormatOps interface {
	Lint(ctx context.Context, dir string, files []string) (output string, exitCode int, err error)
}

// SwiftLintOps is the swiftlint adapter surface needed by the lint use case.
type SwiftLintOps interface {
	Lint(ctx context.Context, in SwiftLintRunInput) (output string, exitCode int, err error)
}

// SwiftLintRunInput mirrors swiftlint.LintInput; declared here so the
// app layer doesn't have to import the adapter package. The CLI-side
// runner converts between this and the adapter's named type.
type SwiftLintRunInput struct {
	Dir        string
	ConfigPath string
}

// SwiftFilesLister enumerates the Swift files swift-format should lint.
// Implemented by the git adapter (`git ls-files -- <pattern>`).
//
//nolint:iface // consumer-defined narrow port — same shape as
// XcodeSecurityOps but semantically a different role (git vs security
// CLI). Merging would couple unrelated adapters.
type SwiftFilesLister interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// SwiftFormatLintInput drives SwiftFormatLint.
type SwiftFormatLintInput struct {
	// Dir is the working directory the linter runs in.
	Dir string
	// FilePattern is forwarded to `git ls-files`. Defaults to `*.swift`.
	FilePattern string
}

// SwiftFormatLint enumerates Swift files via `git ls-files`, runs
// `swift-format lint -s`, prints raw output to w, appends a
// markdown block to the step summary, and returns ErrValidation when
// the linter found issues.
func SwiftFormatLint(
	ctx context.Context,
	files SwiftFilesLister,
	sf SwiftFormatOps,
	summary ci.SummarySink,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in SwiftFormatLintInput,
) error {
	pattern := in.FilePattern
	if pattern == "" {
		pattern = "*.swift"
	}

	listed, err := files.Run(ctx, "ls-files", "--", pattern)
	if err != nil {
		return fmt.Errorf("git ls-files %q: %w", pattern, err)
	}

	swiftFiles := splitNonEmptyLines(listed)
	if len(swiftFiles) == 0 {
		_, _ = fmt.Fprintf(w, "No Swift files found matching pattern: %s\n", pattern)

		return appendSwiftFormatBlock(ctx, summary, build.SwiftLintNoFiles, "")
	}

	_, _ = fmt.Fprintf(w, "Running swift-format on %d files...\n", len(swiftFiles))

	output, exitCode, err := sf.Lint(ctx, in.Dir, swiftFiles)
	if err != nil {
		return fmt.Errorf("swift-format lint: %w", err)
	}

	_, _ = fmt.Fprint(w, output)

	if !strings.HasSuffix(output, "\n") {
		_, _ = fmt.Fprintln(w)
	}

	if exitCode != 0 {
		annot.Errorf("swift-format found formatting issues")

		if err := appendSwiftFormatBlock(ctx, summary, build.SwiftLintFailed, output); err != nil {
			return err
		}

		return fmt.Errorf("swift-format reported issues: %w", errs.ErrValidation)
	}

	_, _ = fmt.Fprintln(w, "✓ swift-format passed")

	return appendSwiftFormatBlock(ctx, summary, build.SwiftLintPassed, "")
}

func appendSwiftFormatBlock(ctx context.Context, summary ci.SummarySink, outcome build.SwiftLintOutcome, body string) error {
	return summary.Append(ctx, build.RenderSwiftFormatBlock(outcome, body))
}

// SwiftLintLintInput drives SwiftLintLint.
type SwiftLintLintInput struct {
	Dir           string
	ConfigPath    string
	FailOnWarning bool
}

// SwiftLintLint runs SwiftLint, prints raw output, appends a markdown
// block, and returns ErrValidation when FailOnWarning is set and the
// linter found issues.
func SwiftLintLint(
	ctx context.Context,
	sl SwiftLintOps,
	summary ci.SummarySink,
	w, stderr io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	annot output.Annotator,
	in SwiftLintLintInput,
) error {
	configPath := in.ConfigPath
	if configPath != "" {
		if exists, err := fileExists(configPath); err != nil {
			return err
		} else if exists {
			_, _ = fmt.Fprintf(w, "Using SwiftLint configuration: %s\n", configPath)
		} else {
			_, _ = fmt.Fprintln(w, "No SwiftLint configuration found, using defaults")

			configPath = ""
		}
	}

	output, exitCode, err := sl.Lint(ctx, SwiftLintRunInput{Dir: in.Dir, ConfigPath: configPath})
	if err != nil {
		return fmt.Errorf("swiftlint lint: %w", err)
	}

	_, _ = fmt.Fprint(w, output)

	if !strings.HasSuffix(output, "\n") {
		_, _ = fmt.Fprintln(w)
	}

	if exitCode == 0 {
		_, _ = fmt.Fprintln(w, "✓ SwiftLint passed")

		return summary.Append(ctx, build.RenderSwiftLintBlock(build.SwiftLintPassed, ""))
	}

	if in.FailOnWarning {
		annot.Errorf("SwiftLint found issues")

		if err := summary.Append(ctx, build.RenderSwiftLintBlock(build.SwiftLintFailed, output)); err != nil {
			return err
		}

		return fmt.Errorf("swiftlint reported issues: %w", errs.ErrValidation)
	}

	annot.Warningf("SwiftLint found issues but fail-on-warning is false")

	return summary.Append(ctx, build.RenderSwiftLintBlock(build.SwiftLintWarned, output))
}

func splitNonEmptyLines(value string) []string {
	out := make([]string, 0)

	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}

	return out
}

// fileExists reports whether path resolves to a regular file.
func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}

	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return false, fmt.Errorf("stat %s: %w", path, err)
}
