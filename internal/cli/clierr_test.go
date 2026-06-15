// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli //nolint:testpackage // exercises the unexported message rewriter directly.

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func TestRewriteHelpTopicMessage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no suggestion",
			in:   "No help topic for 'relese'",
			want: "unknown subcommand 'relese'",
		},
		{
			// urfave appends a bare ". <cmd>" hint; we re-label it as an
			// explicit, clig.dev-style suggestion.
			name: "with suggestion",
			in:   "No help topic for 'relese'. release",
			want: `unknown subcommand 'relese'. Did you mean "release"?`,
		},
		{
			name: "unrelated message is untouched",
			in:   "Required flag \"tag\" not set",
			want: "Required flag \"tag\" not set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := rewriteHelpTopicMessage(tc.in); got != tc.want {
				t.Errorf("rewriteHelpTopicMessage(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestClassifyError_UnknownSubcommandIsUsage confirms the rewritten
// message still classifies as a usage error (exit 2), so the friendlier
// wording doesn't change the exit-code contract.
func TestClassifyError_UnknownSubcommandIsUsage(t *testing.T) {
	t.Parallel()

	err := ClassifyError(errors.New("No help topic for 'relese'. release")) //nolint:err113 // simulating urfave's framework string.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want wrapped errs.ErrUsage", err)
	}

	const want = `unknown subcommand 'relese'. Did you mean "release"?: usage error`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}
