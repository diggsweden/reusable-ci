// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// recordingSummary captures appended markdown blocks for assertion.
type recordingSummary struct {
	buf bytes.Buffer
}

func (r *recordingSummary) Append(_ context.Context, s string) error {
	r.buf.WriteString(s)

	return nil
}

func (r *recordingSummary) String() string { return r.buf.String() }

func newSummary(t *testing.T) *recordingSummary {
	t.Helper()

	return &recordingSummary{}
}

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

	sink := newSummary(t)
	if err := appbuild.SwiftFormatLint(context.Background(), files, &fakeSwiftFormat{}, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.String(), "## Swift Format ⊘") {
		t.Errorf("expected no-files block, got:\n%s", sink.String())
	}
}

func TestSwiftFormatLint_PassedAppendsCheck(t *testing.T) {
	files := &fakeFiles{listed: "a.swift\nb.swift"}
	sf := &fakeSwiftFormat{}

	sink := newSummary(t)
	if err := appbuild.SwiftFormatLint(context.Background(), files, sf, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{}); err != nil {
		t.Fatal(err)
	}

	if len(sf.files) != 1 || len(sf.files[0]) != 2 {
		t.Errorf("expected one invocation with 2 files, got %v", sf.files)
	}

	if !strings.Contains(sink.String(), "## Swift Format ✓") {
		t.Errorf("expected passed block:\n%s", sink.String())
	}
}

func TestSwiftFormatLint_FailedReturnsValidationError(t *testing.T) {
	files := &fakeFiles{listed: "a.swift"}
	sf := &fakeSwiftFormat{output: "a.swift:1:1: line too long", exitCode: 1}
	sink := newSummary(t)

	err := appbuild.SwiftFormatLint(context.Background(), files, sf, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftFormatLintInput{})
	if err == nil || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(sink.String(), "## Swift Format Issues 🔴") {
		t.Errorf("expected failed block:\n%s", sink.String())
	}
}

func TestSwiftLintLint_PassedAppendsCheck(t *testing.T) {
	sl := &fakeSwiftLint{}

	sink := newSummary(t)
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.String(), "## SwiftLint ✓") {
		t.Errorf("expected passed block:\n%s", sink.String())
	}
}

func TestSwiftLintLint_NonZeroWithoutFailOnWarningIsWarning(t *testing.T) {
	sl := &fakeSwiftLint{output: "x", exitCode: 2}

	sink := newSummary(t)
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.String(), "## SwiftLint Warnings ⚠️") {
		t.Errorf("expected warning block:\n%s", sink.String())
	}
}

func TestSwiftLintLint_FailOnWarningTurnsWarningIntoError(t *testing.T) {
	sl := &fakeSwiftLint{output: "x", exitCode: 2}
	sink := newSummary(t)

	err := appbuild.SwiftLintLint(context.Background(), sl, sink, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appbuild.SwiftLintLintInput{FailOnWarning: true})
	if err == nil || !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(sink.String(), "## SwiftLint Issues 🔴") {
		t.Errorf("expected failed block:\n%s", sink.String())
	}
}

func TestSwiftLintLint_MissingConfigFallsThroughToDefaults(t *testing.T) {
	fsys := testfs.NewReal(t)
	sl := &fakeSwiftLint{}
	sink := newSummary(t)

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

	sink := newSummary(t)
	if err := appbuild.SwiftLintLint(context.Background(), sl, sink, io.Discard, io.Discard, output.Annotator{}, appbuild.SwiftLintLintInput{
		ConfigPath: configPath,
	}); err != nil {
		t.Fatal(err)
	}

	if len(sl.configs) != 1 || sl.configs[0] != configPath {
		t.Errorf("expected ConfigPath %q, got %v", configPath, sl.configs)
	}
}
