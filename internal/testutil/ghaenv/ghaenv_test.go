// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ghaenv_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
)

func TestSetup_ExportsEnvVars(t *testing.T) {
	e := ghaenv.Setup(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	if got := os.Getenv("GITHUB_OUTPUT"); got != e.OutputPath {
		t.Errorf("GITHUB_OUTPUT = %q, want %q", got, e.OutputPath)
	}

	if got := os.Getenv("GITHUB_STEP_SUMMARY"); got != e.SummaryPath {
		t.Errorf("GITHUB_STEP_SUMMARY = %q, want %q", got, e.SummaryPath)
	}

	if got := os.Getenv("GITHUB_ACTIONS"); got != "true" {
		t.Errorf("GITHUB_ACTIONS = %q, want %q", got, "true")
	}

	e.Setenv("CUSTOM_GHA_ENV", "set")

	if got := os.Getenv("CUSTOM_GHA_ENV"); got != "set" {
		t.Errorf("CUSTOM_GHA_ENV = %q, want set", got)
	}
}

func TestOutput_ReadsScalar(t *testing.T) {
	e := ghaenv.Setup(t)                                                                //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	require.NoError(t, os.WriteFile(e.OutputPath, []byte("foo=bar\nbaz=qux\n"), 0o644)) //nolint:gosec // test fixture

	if got := e.Output("foo"); got != "bar" {
		t.Errorf("Output(foo) = %q, want %q", got, "bar")
	}

	if got := e.Output("baz"); got != "qux" {
		t.Errorf("Output(baz) = %q, want %q", got, "qux")
	}

	if got := e.Output("missing"); got != "" {
		t.Errorf("Output(missing) = %q, want empty", got)
	}
}

func TestMultiline_ReadsHeredoc(t *testing.T) {
	e := ghaenv.Setup(t) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	heredoc := strings.Join([]string{
		"tags<<EOF_xyz",
		"ghcr.io/x/y:1.2.3",
		"ghcr.io/x/y:1.2",
		"ghcr.io/x/y:1",
		"EOF_xyz",
		"version=1.2.3",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(e.OutputPath, []byte(heredoc), 0o644)) //nolint:gosec // test fixture

	got := e.Multiline("tags")

	want := "ghcr.io/x/y:1.2.3\nghcr.io/x/y:1.2\nghcr.io/x/y:1"
	if got != want {
		t.Errorf("Multiline(tags) =\n%q\nwant\n%q", got, want)
	}
	// Scalar reads still work
	if got := e.Output("version"); got != "1.2.3" {
		t.Errorf("Output(version) = %q, want %q", got, "1.2.3")
	}
}

func TestSummary_ReadsAll(t *testing.T) {
	e := ghaenv.Setup(t)
	require.NoError(t, os.WriteFile(e.SummaryPath, []byte("# Header\n\nbody line\n"), 0o644)) //nolint:gosec // test fixture

	got := e.Summary()
	if !strings.Contains(got, "Header") || !strings.Contains(got, "body line") {
		t.Errorf("Summary() = %q, missing expected content", got)
	}
}
