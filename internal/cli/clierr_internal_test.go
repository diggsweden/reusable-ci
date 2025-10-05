// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli //nolint:testpackage // exercises the unexported message rewriter directly.

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestRewriteHelpTopicMessage_RelabelsUnknownSubcommandsOnly(t *testing.T) {
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

// TestClassifyError_EveryUsageMessageAndItsExitCode covers each framework
// string the classifier recognises, and the exit code it produces.
//
// One case was covered. The rest decide whether a user's typo exits 64 (usage —
// "you typed it wrong") or 70 (software — "file a bug"), and a missing prefix
// sends every instance of that mistake to the wrong one. The exit code is the
// only part a CI job acts on.
func TestClassifyError_EveryUsageMessageAndItsExitCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   error
		want errs.ExitCodeType
	}{
		{
			name: "a required flag was omitted",
			in:   errors.New(`Required flag "artifact" not set`), //nolint:err113 // reproducing a framework string.
			want: errs.ExitCodeUsage,
		},
		{
			name: "an undefined flag was passed",
			in:   errors.New("flag provided but not defined: -nope"), //nolint:err113 // reproducing a framework string.
			want: errs.ExitCodeUsage,
		},
		{
			name: "an unknown subcommand reached the help topic path",
			in:   errors.New("No help topic for 'relese'"), //nolint:err113 // reproducing a framework string.
			want: errs.ExitCodeUsage,
		},
		{
			name: "an already-relabelled unknown subcommand",
			in:   errors.New("unknown subcommand 'relese'"), //nolint:err113 // reproducing a framework string.
			want: errs.ExitCodeUsage,
		},
		{
			// Anything else must pass through untouched: classifying a
			// genuine failure as usage would tell the operator they typed
			// something wrong when the run actually broke.
			name: "an ordinary failure is not reclassified",
			in:   errs.ErrDependencyUnavailable,
			want: errs.ExitCodeFromError(errs.ErrDependencyUnavailable),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyError(tc.in)
			if got == nil {
				t.Fatal("ClassifyError returned nil for a non-nil error")
			}

			if code := errs.ExitCodeFromError(got); code != tc.want {
				t.Errorf("exit code = %d, want %d (err = %v)", code, tc.want, got)
			}
		})
	}

	if ClassifyError(nil) != nil {
		t.Error("ClassifyError(nil) must stay nil")
	}
}

// TestIsAlreadyPrintedByCLI_MatchesOnlyWhatTheFrameworkPrints pins the split
// between the two sets, which is easy to get wrong in the direction that
// produces silence.
//
// urfave/cli prints some of its own usage errors before returning them, and
// main suppresses those to avoid printing twice. The other usage messages it
// does NOT print, so main must. Saying "already printed" about one of those
// leaves the user with a non-zero exit and no message at all — the worst
// outcome of the four, and indistinguishable from a crash.
func TestIsAlreadyPrintedByCLI_MatchesOnlyWhatTheFrameworkPrints(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   error
		want bool
	}{
		{
			name: "a required flag is printed by the framework",
			in:   errors.New(`Required flag "artifact" not set`), //nolint:err113 // reproducing a framework string.
			want: true,
		},
		{
			name: "an undefined flag is printed by the framework",
			in:   errors.New("flag provided but not defined: -nope"), //nolint:err113 // reproducing a framework string.
			want: true,
		},
		{
			name: "a help-topic error is NOT printed, so main must print it",
			in:   errors.New("No help topic for 'relese'"), //nolint:err113 // reproducing a framework string.
		},
		{
			name: "a relabelled unknown subcommand is NOT printed either",
			in:   errors.New("unknown subcommand 'relese'"), //nolint:err113 // reproducing a framework string.
		},
		{name: "an ordinary failure is not printed", in: errs.ErrDependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := IsAlreadyPrintedByCLI(tc.in); got != tc.want {
				t.Errorf("IsAlreadyPrintedByCLI = %v, want %v", got, tc.want)
			}
		})
	}

	if IsAlreadyPrintedByCLI(nil) {
		t.Error("IsAlreadyPrintedByCLI(nil) must be false")
	}
}

// TestAlreadyPrintedIsASubsetOfUsage keeps the two lists from drifting apart.
// Everything the framework prints is a usage message; the reverse is not true,
// and that asymmetry is the whole reason there are two functions.
func TestAlreadyPrintedIsASubsetOfUsage(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{
		`Required flag "artifact" not set`,
		"flag provided but not defined: -nope",
	} {
		if !isUsageMessage(msg) {
			t.Errorf("%q is treated as already printed but not as a usage message; "+
				"it would be suppressed and then exit 70", msg)
		}
	}
}
