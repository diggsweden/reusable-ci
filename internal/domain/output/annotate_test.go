// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package output_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
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
		output.FormatText, output.FormatJSON, output.FormatGitLab, output.FormatAuto,
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

func TestAnnotator_ZeroValue_DiscardsWrites(t *testing.T) {
	t.Parallel()
	// Zero-value Annotator (no writer set) should not panic.
	var a output.Annotator
	a.Errorf("boom")
	a.Warningf("watch out")
	a.Noticef("FYI")
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

func TestAnnotatorFromFlag_ResolvesFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	a := output.AnnotatorFromFlag(&buf, "auto", provider.PlatformGitHub) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	a.Warningf("watch out")
	require.Equal(t, "::warning::watch out\n", buf.String())

	buf.Reset()
	a = output.AnnotatorFromFlag(&buf, "bogus", provider.PlatformGitHub)
	a.Warningf("watch out")
	require.Equal(t, "Warning: watch out\n", buf.String())
}
