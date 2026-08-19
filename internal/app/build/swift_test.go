// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// fakeFiles records git args and returns canned output.
type fakeFiles struct {
	listed string
	err    error
	args   [][]string
}

func (f *fakeFiles) Run(_ context.Context, args ...string) (string, error) {
	f.args = append(f.args, args)

	return f.listed, f.err
}

// fakeSwiftFormat returns canned output + exit code.
type fakeSwiftFormat struct {
	output   string
	exitCode int
	err      error
	files    [][]string
}

func (f *fakeSwiftFormat) Lint(_ context.Context, _ string, files []string) (string, int, error) {
	f.files = append(f.files, files)

	return f.output, f.exitCode, f.err
}

// fakeSwiftLint returns canned output + exit code.
type fakeSwiftLint struct {
	output   string
	exitCode int
	err      error
	configs  []string
}

func (f *fakeSwiftLint) Lint(_ context.Context, in appbuild.SwiftLintRunInput) (string, int, error) {
	f.configs = append(f.configs, in.ConfigPath)

	return f.output, f.exitCode, f.err
}

func TestSwiftFormatLint_NoFilesIsPassThrough(t *testing.T) {
	files := &fakeFiles{listed: ""}

	sink := &recordingSummarySink{}
	if err := appbuild.SwiftFormatLint(context.Background(), files, &fakeSwiftFormat{}, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "## Swift Format ⊘") {
		t.Errorf("expected no-files block, got:\n%s", sink.buf.String())
	}
}

func TestSwiftFormatLint_PassedAppendsCheck(t *testing.T) {
	files := &fakeFiles{listed: "a.swift\nb.swift"}
	sf := &fakeSwiftFormat{}

	sink := &recordingSummarySink{}
	if err := appbuild.SwiftFormatLint(context.Background(), files, sf, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{}); err != nil {
		t.Fatal(err)
	}

	// Which files, not how many: a count passes even if the listing was
	// mangled into two wrong names.
	if len(sf.files) != 1 || !reflect.DeepEqual(sf.files[0], []string{"a.swift", "b.swift"}) {
		t.Errorf("swift-format files = %v, want one invocation with [a.swift b.swift]", sf.files)
	}

	if !strings.Contains(sink.buf.String(), "## Swift Format ✓") {
		t.Errorf("expected passed block:\n%s", sink.buf.String())
	}
}

// TestSwiftFormatLint_ListsFilesSafely pins the git invocation the file
// list comes from. fakeFiles has always recorded it and nothing ever
// looked, so neither the default pattern nor the "--" separating it from
// the option list had a test -- and --file-pattern is caller-supplied, so
// that separator is what keeps a value beginning with a dash an argument
// rather than an option.
func TestSwiftFormatLint_ListsFilesSafely(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name: "default pattern",
			want: []string{"ls-files", "--", "*.swift"},
		},
		{
			name:    "caller pattern",
			pattern: "Sources/**/*.swift",
			want:    []string{"ls-files", "--", "Sources/**/*.swift"},
		},
		{
			name:    "a pattern that looks like an option stays an argument",
			pattern: "--exclude-standard",
			want:    []string{"ls-files", "--", "--exclude-standard"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFiles{listed: "a.swift"}

			if err := appbuild.SwiftFormatLint(context.Background(), files, &fakeSwiftFormat{}, &recordingSummarySink{}, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{FilePattern: tc.pattern}); err != nil {
				t.Fatal(err)
			}

			if len(files.args) != 1 || !reflect.DeepEqual(files.args[0], tc.want) {
				t.Errorf("git args = %v, want one call %v", files.args, tc.want)
			}
		})
	}
}

func TestSwiftFormatLint_FailedReturnsValidationError(t *testing.T) {
	files := &fakeFiles{listed: "a.swift"}
	sf := &fakeSwiftFormat{output: "a.swift:1:1: line too long", exitCode: 1}
	sink := &recordingSummarySink{}

	err := appbuild.SwiftFormatLint(context.Background(), files, sf, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{})
	if err == nil || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(sink.buf.String(), "## Swift Format Issues 🔴") {
		t.Errorf("expected failed block:\n%s", sink.buf.String())
	}
}

func TestSwiftLintLint_PassedAppendsCheck(t *testing.T) {
	sl := &fakeSwiftLint{}

	sink := &recordingSummarySink{}
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "## SwiftLint ✓") {
		t.Errorf("expected passed block:\n%s", sink.buf.String())
	}
}

func TestSwiftLintLint_NonZeroWithoutFailOnWarningIsWarning(t *testing.T) {
	sl := &fakeSwiftLint{output: "x", exitCode: 2}

	sink := &recordingSummarySink{}
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "## SwiftLint Warnings ⚠️") {
		t.Errorf("expected warning block:\n%s", sink.buf.String())
	}
}

func TestSwiftLintLint_FailOnWarningTurnsWarningIntoError(t *testing.T) {
	sl := &fakeSwiftLint{output: "x", exitCode: 2}
	sink := &recordingSummarySink{}

	err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{FailOnWarning: true})
	if err == nil || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(sink.buf.String(), "## SwiftLint Issues 🔴") {
		t.Errorf("expected failed block:\n%s", sink.buf.String())
	}
}

func TestSwiftLintLint_MissingConfigFallsThroughToDefaults(t *testing.T) {
	fsys := testfs.NewReal(t)
	sl := &fakeSwiftLint{}
	sink := &recordingSummarySink{}

	var out bytes.Buffer
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, &out, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{
		ConfigPath: filepath.Join(fsys.Root, "missing-config.yml"),
	}); err != nil {
		t.Fatal(err)
	}

	if len(sl.configs) != 1 || sl.configs[0] != "" {
		t.Errorf("expected empty ConfigPath forwarded (config fell through), got %v", sl.configs)
	}

	if !strings.Contains(out.String(), "No SwiftLint configuration found") {
		t.Errorf("expected fall-through log, got:\n%s", out.String())
	}
}

func TestSwiftLintLint_PresentConfigIsForwarded(t *testing.T) {
	fsys := testfs.NewReal(t)
	configPath := fsys.WriteFile(".swiftlint.yml", []byte("disabled_rules: []\n"))
	sl := &fakeSwiftLint{}

	sink := &recordingSummarySink{}
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftLintLintInput{
		ConfigPath: configPath,
	}); err != nil {
		t.Fatal(err)
	}

	if len(sl.configs) != 1 || sl.configs[0] != configPath {
		t.Errorf("expected ConfigPath %q, got %v", configPath, sl.configs)
	}
}
