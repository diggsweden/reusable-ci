// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package output_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestAnnotator_GitHubFormat_EmitsWorkflowCommands(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.NewAnnotator(&buf, output.FormatGitHub)

	a.Errorf("missing %s", "X")
	a.Warningf("watch out")
	a.Noticef("FYI")

	require.Equal(t,
		"::error::missing X\n::warning::watch out\n::notice::FYI\n",
		buf.String(),
	)
}

func TestAnnotator_NonGitHubFormats_EmitPlainPrefix(t *testing.T) {
	t.Parallel()

	for _, f := range []output.Format{ //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		output.FormatText, output.FormatJSON, output.FormatGitLab, output.FormatForgejo, output.FormatAuto,
	} {
		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			a := output.NewAnnotator(&buf, f)
			a.Errorf("boom")
			a.Warningf("careful")
			a.Noticef("FYI")
			require.Equal(t, "Error: boom\nWarning: careful\nNotice: FYI\n", buf.String())
		})
	}
}

// TestAnnotator_ZeroValue_DiscardsWrites covers the `if a.w == nil` guard that
// opens both emit and emitAt.
//
// There is no assertion because a panic is the failure: a zero-value Annotator
// carries a nil io.Writer, so dropping either guard makes every call here a nil
// dereference. The located variants go through emitAt, which has its own copy
// of the check -- the other ErrorAt and WarningAt tests all supply a real
// writer, so without these two lines that second guard is unguarded.
func TestAnnotator_ZeroValue_DiscardsWrites(t *testing.T) {
	t.Parallel()

	var a output.Annotator

	a.Errorf("boom")
	a.Warningf("watch out")
	a.Noticef("FYI")
	a.ErrorAt(output.Annotation{File: "f.yml", Line: 6}, "bad value")
	a.WarningAt(output.Annotation{Title: "Heads up"}, "watch out")
}

func TestAnnotator_FormatString_PassedThrough(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.NewAnnotator(&buf, output.FormatGitHub)
	a.Errorf("HTTP %d: %s", 500, "bad gateway")
	require.Equal(t, "::error::HTTP 500: bad gateway\n", buf.String())
}

func TestAnnotator_GitHubFormat_EscapesWorkflowCommandData(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.NewAnnotator(&buf, output.FormatGitHub)

	a.Warningf("first line\n100%% done\rnext")

	require.Equal(t, "::warning::first line%0A100%25 done%0Dnext\n", buf.String())
}

func TestAnnotator_ErrorAt_GitHubRendersEscapedProperties(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.NewAnnotator(&buf, output.FormatGitHub)
	// Hostile file path (comma/colon) + hostile message (newline) must be
	// escaped so neither can forge a property or a second command.
	a.ErrorAt(output.Annotation{File: "a,b:c.yml", Line: 6}, "bad: %s", "x\n::error::forged")

	require.Equal(t,
		"::error file=a%2Cb%3Ac.yml,line=6::bad: x%0A::error::forged\n",
		buf.String())
}

func TestAnnotator_ErrorAt_NonGitHubRendersPlainLocation(t *testing.T) {
	t.Parallel()

	for _, format := range []output.Format{output.FormatText, output.FormatForgejo} {
		var buf bytes.Buffer

		a := output.NewAnnotator(&buf, format)
		a.ErrorAt(output.Annotation{File: "f.yml", Line: 6}, "bad value")
		a.WarningAt(output.Annotation{File: "f.yml"}, "odd value")

		require.Equal(t, "Error: f.yml:6: bad value\nWarning: f.yml: odd value\n", buf.String(), "format %s", format)
	}
}

// TestAnnotator_PlainFormats_KeepDataOnTheAnnotationLine is the plain
// branch's answer to annotation injection. GitHub's branch escapes control
// characters; the plain branch used to print data verbatim, so a value
// carrying a line break started a fresh line at column 0, which is where a
// runner that understands `::command::` lines (Forgejo's act_runner does)
// looks for one. Continuation lines are indented, so none can be a command.
func TestAnnotator_PlainFormats_KeepDataOnTheAnnotationLine(t *testing.T) {
	t.Parallel()

	for _, format := range []output.Format{output.FormatText, output.FormatForgejo, output.FormatGitLab} {
		var buf bytes.Buffer

		a := output.NewAnnotator(&buf, format)
		a.Errorf("tag %s refused", "v1\n::add-mask::x\r::stop-commands::y\r\n::error::z")
		a.ErrorAt(output.Annotation{File: "f.yml"}, "bad\n::warning::w")

		for _, line := range strings.Split(buf.String(), "\n") {
			require.False(t, strings.HasPrefix(line, "::"), "format %s let data start a command line:\n%s", format, buf.String())
		}

		require.Equal(t, "Error: tag v1\n    ::add-mask::x\n    ::stop-commands::y\n    ::error::z refused\nError: f.yml: bad\n    ::warning::w\n", buf.String())
	}
}

func TestAnnotator_WarningAt_TitleOnly(t *testing.T) {
	t.Parallel()

	var gh, plain bytes.Buffer

	output.NewAnnotator(&gh, output.FormatGitHub).WarningAt(output.Annotation{Title: "Heads up"}, "watch out")
	output.NewAnnotator(&plain, output.FormatText).WarningAt(output.Annotation{Title: "Heads up"}, "watch out")

	require.Equal(t, "::warning title=Heads up::watch out\n", gh.String())
	require.Equal(t, "Warning: watch out\n", plain.String())
}

// TestEscapeWorkflowCommand_* lock the exported escapers used by the
// hand-built `::error::`/`::warning::` emitters (validate/version) so hostile
// interpolated content cannot forge additional workflow commands.
func TestEscapeWorkflowCommandData_NeutralisesInjection(t *testing.T) {
	t.Parallel()

	// A payload trying to close the command and start a forged one.
	got := output.EscapeWorkflowCommandData("ok\n::error::forged 100% \r")
	require.Equal(t, "ok%0A::error::forged 100%25 %0D", got)
	require.NotContains(t, got, "\n", "a newline would let the payload start a new workflow command")
}

func TestEscapeWorkflowCommandProperty_EscapesSeparators(t *testing.T) {
	t.Parallel()

	// Property values additionally escape ":" and "," so a value can't
	// terminate the property list or the command header.
	got := output.EscapeWorkflowCommandProperty("a,b:c\nd%")
	require.Equal(t, "a%2Cb%3Ac%0Ad%25", got)
}

func TestAnnotatorFromFlag_ResolvesFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.AnnotatorFromFlag(&buf, "auto", provider.RunnerGitHub) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	a.Warningf("watch out")
	require.Equal(t, "::warning::watch out\n", buf.String())

	buf.Reset()
	a = output.AnnotatorFromFlag(&buf, "bogus", provider.RunnerGitHub)
	a.Warningf("watch out")
	require.Equal(t, "Warning: watch out\n", buf.String())

	// Forgejo resolves to FormatForgejo, which takes the plain branch —
	// no unrenderable ::warning:: workflow command (go-gitea/gitea#27898).
	buf.Reset()
	a = output.AnnotatorFromFlag(&buf, "auto", provider.RunnerForgejo)
	a.Warningf("watch out")
	require.Equal(t, "Warning: watch out\n", buf.String())
}

// TestAnnotator_ConcurrentSerialisesWrites covers the wrapper that makes one
// Annotator safe to hand to work running in parallel.
//
// An Annotator is a writer and a format, and nothing serialised the writer.
// validate.Prerequisites runs its checks in an errgroup and gives each the same
// annotator, so two checks that both annotate write to one io.Writer from two
// goroutines. On a runner the visible symptom is two "::error::" lines spliced
// into each other, which the forge parses as neither.
//
// It went unnoticed because every test passed the zero-value Annotator, whose
// writer is nil and whose writes go nowhere — the race needs a real
// destination, and no test had one.
func TestAnnotator_ConcurrentSerialisesWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	annot := output.NewAnnotator(&buf, output.FormatGitHub).Concurrent()

	const writers = 16

	var wg sync.WaitGroup

	for i := range writers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			annot.Errorf("message %d", i)
		}()
	}

	wg.Wait()

	// Every annotation must be a whole line: a spliced write shows up as a
	// line that does not carry the full prefix.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != writers {
		t.Fatalf("got %d annotation lines, want %d:\n%s", len(lines), writers, buf.String())
	}

	for i, line := range lines {
		if !strings.HasPrefix(line, "::error::") {
			t.Errorf("line %d is spliced: %q", i, line)
		}
	}
}

// TestAnnotator_ConcurrentOnTheZeroValueIsSafe keeps the wrapper usable
// unconditionally: the zero-value Annotator has no writer to guard.
func TestAnnotator_ConcurrentOnTheZeroValueIsSafe(t *testing.T) {
	t.Parallel()

	var annot output.Annotator

	annot.Concurrent().Errorf("no destination configured")
}
