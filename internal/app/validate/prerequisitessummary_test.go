// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// WritePrerequisitesSummary was uncovered, along with collectWarnings
// and errorOneLine. It is the panel an operator reads to decide whether
// a release is safe to let through, so what it does and does not say is
// the whole product here.
//
// The warnings half matters most. The validators emit ⚠️ lines for
// things that are non-fatal but security-relevant -- "no signer
// allowlist enforced", "signature could not be verified" -- and this is
// what lifts them out of the log into a callout. A summary that dropped
// them would show a table of ticks for a release that skipped its
// signer checks.

// recordingSink captures what the summary writer appends.
type recordingSink struct{ appended []string }

func (s *recordingSink) Append(_ context.Context, markdown string) error {
	s.appended = append(s.appended, markdown)

	return nil
}

func summaryFor(t *testing.T, checks ...appvalidate.ValidatorOutcome) string {
	t.Helper()

	sink := &recordingSink{}
	if err := appvalidate.WritePrerequisitesSummary(context.Background(), sink,
		appvalidate.PrerequisitesResult{Checks: checks}); err != nil {
		t.Fatal(err)
	}

	if len(sink.appended) != 1 {
		t.Fatalf("summary appended %d times, want 1", len(sink.appended))
	}

	return sink.appended[0]
}

func TestWritePrerequisitesSummary_RendersEachStatus(t *testing.T) {
	t.Parallel()

	body := summaryFor(t,
		appvalidate.ValidatorOutcome{Name: "tag-format"},
		appvalidate.ValidatorOutcome{Name: "tag-signature", Err: errs.ErrPermissionDenied},
		appvalidate.ValidatorOutcome{Name: "cargo", Skipped: true, SkipReason: "no Cargo.toml"},
	)

	for _, want := range []string{
		"| tag-format | ✓ Passed |",
		"| tag-signature | ✗ Failed |",
		"| cargo | − Skipped (no Cargo.toml) |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing row %q in:\n%s", want, body)
		}
	}

	// A skip is not a pass. The three statuses are what an operator
	// scans, and a skipped signer check rendered as a tick is the exact
	// misreading this table exists to prevent.
	if strings.Contains(body, "| cargo | ✓ Passed |") {
		t.Errorf("a skipped check was rendered as passed:\n%s", body)
	}
}

// TestWritePrerequisitesSummary_LiftsWarningsIntoACallout closes the
// loop with the validators: they write ⚠️ lines into their per-check
// output, and this is what makes those lines visible.
func TestWritePrerequisitesSummary_LiftsWarningsIntoACallout(t *testing.T) {
	t.Parallel()

	body := summaryFor(t,
		appvalidate.ValidatorOutcome{
			Name:   "tag-signature",
			Output: "Tag v1.0.0 is signed\n⚠️ No GPG signer allowlist present (.reusable-ci/allowed_gpg_keys.asc) — enforcement skipped\n",
		},
		appvalidate.ValidatorOutcome{
			Name:   "release-authorization",
			Output: "  ⚠️ GPG signature could not be verified against the allowlist\n",
		},
	)

	if !strings.Contains(body, "> [!WARNING]") {
		t.Fatalf("the warnings were not lifted into a callout:\n%s", body)
	}

	// Both, not just the first: two validators can each skip a check,
	// and a callout naming one of them understates what was skipped.
	for _, want := range []string{"enforcement skipped", "could not be verified"} {
		if !strings.Contains(body, "> ⚠️") || !strings.Contains(body, want) {
			t.Errorf("warning %q missing from the callout:\n%s", want, body)
		}
	}

	// A run with warnings but no failures is still a pass; the callout
	// must not manufacture a failure section.
	if strings.Contains(body, "One or more prerequisites failed") {
		t.Errorf("warnings were reported as failures:\n%s", body)
	}
}

// TestWritePrerequisitesSummary_NoWarningsNoCallout is the other side:
// an empty callout header on a clean run trains operators to ignore it.
func TestWritePrerequisitesSummary_NoWarningsNoCallout(t *testing.T) {
	t.Parallel()

	body := summaryFor(t, appvalidate.ValidatorOutcome{Name: "tag-format", Output: "Tag v1.0.0 is well formed\n"})
	if strings.Contains(body, "[!WARNING]") {
		t.Errorf("a clean run emitted a warning callout:\n%s", body)
	}
}

// TestWritePrerequisitesSummary_FailuresAreListedOnOneLineEach covers
// errorOneLine. Validators wrap multi-line tool output, and a summary
// table that inherited a hundred lines of gpg diagnostics would push
// every other row off the screen.
func TestWritePrerequisitesSummary_FailuresAreListedOnOneLineEach(t *testing.T) {
	t.Parallel()

	multiline := errors.New("verify failed\n  gpg: Signature made Tue\n  gpg: Can't check signature") //nolint:err113 // fixture error.

	body := summaryFor(t, appvalidate.ValidatorOutcome{Name: "tag-signature", Err: multiline})

	if !strings.Contains(body, "- **tag-signature**: verify failed") {
		t.Errorf("the failure detail is missing:\n%s", body)
	}

	if strings.Contains(body, "Can't check signature") {
		t.Errorf("the whole multi-line error was inlined into the summary:\n%s", body)
	}
}
