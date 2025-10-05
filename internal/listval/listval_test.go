// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package listval_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/listval"
)

func TestTokens_SplitsOnCommasSpacesAndNewlines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"comma", "a,b,c", []string{"a", "b", "c"}},
		{"spaces", "a b c", []string{"a", "b", "c"}},
		{"newlines", "a\nb\nc", []string{"a", "b", "c"}},
		{"mixed + padding", " a, b\n c ,,d ", []string{"a", "b", "c", "d"}},
		{"empty", "", nil},
		{"only delimiters", " , \n ", nil},
		{"preserves order + dupes", "b,a,b", []string{"b", "a", "b"}},
		{"realistic platforms", "linux/amd64, linux/arm64", []string{"linux/amd64", "linux/arm64"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// slices.Equal treats nil and empty as equal, so the
			// "nothing to tokenise" rows need no special case.
			if got := listval.Tokens(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("Tokens(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestTokens_EveryDelimiterAndOnlyThose covers the two delimiters the table
// above never exercises, and pins the boundary of the set.
//
// Nothing used a tab or a carriage return, so dropping either from the switch
// passed every case. CR is the one that matters in practice: an artifacts.yml
// edited on Windows, or a YAML block scalar round-tripped through a tool that
// normalises line endings, arrives as CRLF. Without CR the tokens keep a
// trailing "\r" — a platform becomes "linux/amd64\r", which no longer matches
// the platform it names and travels into an image tag as an invisible byte.
//
// The other direction is pinned too. This deliberately splits on five ASCII
// characters, not on Unicode whitespace: a token is allowed to contain a
// non-breaking space or a vertical tab, and widening the set to unicode.IsSpace
// would silently start cutting values apart.
func TestTokens_EveryDelimiterAndOnlyThose(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want []string
		why  string
	}{
		{name: "tab", in: "a\tb\tc", want: []string{"a", "b", "c"}},
		{
			name: "carriage return", in: "a\rb\rc", want: []string{"a", "b", "c"},
			why: "a lone CR is a line ending on its own",
		},
		{
			name: "CRLF line endings", in: "a\r\nb\r\nc", want: []string{"a", "b", "c"},
			why: "a Windows-authored list must not leave \\r glued to each token",
		},
		{
			name: "trailing CRLF", in: "linux/amd64\r\n", want: []string{"linux/amd64"},
			why: "the common shape: one value per line, file ends with a newline",
		},
		{name: "tabs mixed with commas and spaces", in: "a,\tb \r\n c", want: []string{"a", "b", "c"}},
		{name: "runs of mixed delimiters collapse", in: "a \t\r\n,, \tb", want: []string{"a", "b"}},
		{name: "only CR and tab is nothing", in: "\t\r\n \t", want: nil},

		// The set is exactly these five characters.
		{
			name: "a non-breaking space is part of the token",
			in:   "a b", want: []string{"a b"},
			why: "widening to Unicode whitespace would cut this in two",
		},
		{
			name: "a vertical tab is part of the token",
			in:   "a\vb", want: []string{"a\vb"},
			why: "ASCII whitespace beyond the listed five is not a delimiter",
		},
		{
			name: "a form feed is part of the token",
			in:   "a\fb", want: []string{"a\fb"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := listval.Tokens(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("Tokens(%q) = %#v, want %#v: %s", tc.in, got, tc.want, tc.why)
			}
		})
	}
}
