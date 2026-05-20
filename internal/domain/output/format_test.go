// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package output_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestAll_IncludesAutoAndConcrete(t *testing.T) {
	t.Parallel()
	require.ElementsMatch(t,
		[]output.Format{
			output.FormatAuto, output.FormatText, output.FormatJSON,
			output.FormatGitHub, output.FormatForgejo, output.FormatGitLab,
		},
		output.All(),
	)
}

func TestParse_RoundTripsEveryAccepted(t *testing.T) {
	t.Parallel()

	for _, f := range output.All() {
		got, err := output.Parse(string(f))
		require.NoError(t, err)
		require.Equal(t, f, got)
	}
}

func TestParse_IsCaseInsensitiveAndTrimsSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want output.Format
	}{
		{"AUTO", output.FormatAuto},
		{"Json", output.FormatJSON},
		{" text ", output.FormatText},
		{"GITHUB", output.FormatGitHub},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := output.Parse(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParse_RejectsUnknown(t *testing.T) {
	t.Parallel()

	_, err := output.Parse("yaml")
	require.ErrorIs(t, err, output.ErrUnknownFormat)
	require.Contains(t, err.Error(), `"yaml"`)
	require.Contains(t, err.Error(), "auto")
	require.Contains(t, err.Error(), "json")
}

func TestParse_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := output.Parse("")
	require.ErrorIs(t, err, output.ErrUnknownFormat)
	require.Contains(t, err.Error(), "empty")
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format output.Format
		runner provider.RunnerKind
		want   output.Format
	}{
		{name: "concrete_text_passes_through", format: output.FormatText, runner: provider.RunnerGitHub, want: output.FormatText},
		{name: "concrete_json_passes_through", format: output.FormatJSON, runner: provider.RunnerGitLab, want: output.FormatJSON},
		{name: "concrete_github_passes_through", format: output.FormatGitHub, runner: provider.RunnerLocal, want: output.FormatGitHub},
		{name: "concrete_gitlab_passes_through", format: output.FormatGitLab, runner: provider.RunnerLocal, want: output.FormatGitLab},
		{name: "auto_on_github", format: output.FormatAuto, runner: provider.RunnerGitHub, want: output.FormatGitHub},
		{name: "auto_on_forgejo", format: output.FormatAuto, runner: provider.RunnerForgejo, want: output.FormatForgejo},
		{name: "auto_on_gitlab", format: output.FormatAuto, runner: provider.RunnerGitLab, want: output.FormatGitLab},
		{name: "auto_on_local", format: output.FormatAuto, runner: provider.RunnerLocal, want: output.FormatText},
		{name: "auto_on_empty_runner", format: output.FormatAuto, runner: "", want: output.FormatText},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, output.Resolve(testCase.format, testCase.runner))
		})
	}
}

func TestParseAndResolve_ResolvesAuto(t *testing.T) {
	t.Parallel()

	got, err := output.ParseAndResolve("auto", provider.RunnerGitHub)
	require.NoError(t, err)
	require.Equal(t, output.FormatGitHub, got)
}

func TestParseAndResolve_PropagatesParseError(t *testing.T) {
	t.Parallel()

	_, err := output.ParseAndResolve("yaml", provider.RunnerLocal)
	require.ErrorIs(t, err, output.ErrUnknownFormat)
}

func TestErrUnknownFormat_IsSentinel(t *testing.T) {
	t.Parallel()

	_, err := output.Parse("xml")
	require.ErrorIs(t, err, output.ErrUnknownFormat)
}
