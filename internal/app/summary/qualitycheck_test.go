// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

// fakeSummarySink records every Append into a buffer.
type fakeSummarySink struct{ buf bytes.Buffer }

func (f *fakeSummarySink) Append(_ context.Context, s string) error {
	f.buf.WriteString(s)

	return nil
}

func TestQualityCheckStatus_AllPass(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.QualityCheckStatus(context.Background(), sink, []appsummary.QualityCheck{
		{Name: "Nanolinter", Enabled: true, Result: domainsummary.ResultSuccess},
		{Name: "OpenGrep SAST", Enabled: true, Result: domainsummary.ResultSuccess},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"## Pull Request Check Status",
		"| Nanolinter | ✓ Pass |",
		"| OpenGrep SAST | ✓ Pass |",
		"### ✓ All enabled checks passed",
	} {
		if !strings.Contains(sink.buf.String(), want) {
			t.Errorf("output missing %q\nfull: %s", want, sink.buf.String())
		}
	}
}

func TestQualityCheckStatus_MixedResults(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.QualityCheckStatus(context.Background(), sink, []appsummary.QualityCheck{
		{Name: "A", Enabled: false, Result: domainsummary.ResultSuccess},
		{Name: "B", Enabled: true, Result: domainsummary.ResultSuccess},
		{Name: "C", Enabled: true, Result: domainsummary.ResultSkipped},
		{Name: "D", Enabled: true, Result: domainsummary.ResultFailure},
	}); err != nil {
		t.Fatal(err)
	}

	wants := []string{
		"| A | 🔸 Disabled |",
		"| B | ✓ Pass |",
		"| C | − Skipped |",
		"| D | ✗ Fail |",
		"### ✗ Some checks failed",
	}
	for _, w := range wants {
		if !strings.Contains(sink.buf.String(), w) {
			t.Errorf("missing %q\nfull: %s", w, sink.buf.String())
		}
	}
}

func TestParseQualityChecks_PreservesMalformedLinesAsUnconfirmed(t *testing.T) {
	t.Parallel()

	got := appsummary.ParseQualityChecks([]string{
		"Nanolinter|true|success",
		"Sast|false|skipped",
		"malformed-no-pipes",
	})
	if len(got) != 3 || !got[2].Enabled || got[2].Result != "" {
		t.Fatalf("malformed line must remain unconfirmed: %+v", got)
	}

	if got[0].Name != "Nanolinter" || !got[0].Enabled || got[0].Result != domainsummary.ResultSuccess {
		t.Errorf("got[0] = %+v", got[0])
	}

	if got[1].Name != "Sast" || got[1].Enabled || got[1].Result != domainsummary.ResultSkipped {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestQualityCheckStatus_NoArgsStillRendersHeaders(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.QualityCheckStatus(context.Background(), sink, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"## Pull Request Check Status", "### Quality Check Results", "### ✓ All enabled checks passed"} {
		if !strings.Contains(sink.buf.String(), want) {
			t.Errorf("missing %q in %s", want, sink.buf.String())
		}
	}
}

// TestQualityCheckStatus_EscapesTableSyntaxInCheckNames covers the cell
// sanitiser on the one input this block does not control. A check name
// carrying a `|` would otherwise close its own cell and let whatever follows
// render as further columns -- a name of "Lint | ✓ Pass" would show up as a
// passing row no matter what its real result was. Backticks and control
// characters are neutralised for the same reason.
//
// The test previously named for "special chars" only checked that "Shell
// Check" and "check-yaml" appeared somewhere in the output, which no
// escaping bug would have broken.
func TestQualityCheckStatus_EscapesTableSyntaxInCheckNames(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.QualityCheckStatus(context.Background(), sink, []appsummary.QualityCheck{
		{Name: "Shell Check", Enabled: true, Result: domainsummary.ResultSuccess},
		{Name: "forged | ✓ Pass", Enabled: true, Result: domainsummary.ResultFailure},
		{Name: "back`tick", Enabled: true, Result: domainsummary.ResultSuccess},
		{Name: "new\nline", Enabled: true, Result: domainsummary.ResultSuccess},
	}); err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"| Shell Check | ✓ Pass |",
		"| forged &#124; ✓ Pass | ✗ Fail |",
		"| back&#96;tick | ✓ Pass |",
		"| new line | ✓ Pass |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}

	// The escaped "✓ Pass" still appears inside the forged name's cell, which
	// is harmless -- what matters is that it did not become a row of its own.
	// Count whole rows that report a pass: three, not four.
	passRows := 0

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| ") && strings.HasSuffix(line, "| ✓ Pass |") {
			passRows++
		}
	}

	if passRows != 3 {
		t.Errorf("%d rows report a pass, want 3 (the forged name leaked a row):\n%s", passRows, body)
	}
}

// TestQualityCheckStatus_AggregateFollowsTheWorstEnabledRow pins the verdict
// line for each combination, which the tests above checked only for all-pass
// and failure. A disabled check never counts; an enabled check that was
// skipped or reported a value that is not a result is not a pass; a failure
// outranks both. The rows keep the order they were given.
func TestQualityCheckStatus_AggregateFollowsTheWorstEnabledRow(t *testing.T) {
	t.Parallel()

	header := "## Pull Request Check Status\n\n### Quality Check Results\n| Check | Status |\n|-------|--------|\n"
	failed := "### ✗ Some checks failed\nPlease review the failures above and fix any issues.\n" +
		"Note: Individual linter failures are shown above. This status job always succeeds to provide summary.\n"

	for name, tc := range map[string]struct {
		records []string
		want    string
	}{
		"disabled does not count": {
			records: []string{"Lint|true|success", "SAST|false|failure"},
			want:    header + "| Lint | ✓ Pass |\n| SAST | 🔸 Disabled |\n\n### ✓ All enabled checks passed\n",
		},
		"skipped is not a pass": {
			records: []string{"Lint|true|success", "SAST|true|skipped"},
			want:    header + "| Lint | ✓ Pass |\n| SAST | − Skipped |\n\n### Some enabled checks are skipped or unconfirmed\n",
		},
		"an unknown result is not a pass": {
			records: []string{"Lint|true|passed", "broken record"},
			want:    header + "| Lint | Unconfirmed |\n| broken record | Unconfirmed |\n\n### Some enabled checks are skipped or unconfirmed\n",
		},
		"failure outranks unconfirmed": {
			records: []string{"Lint|true|skipped", "SAST|true|failure", "Deps|true|"},
			want:    header + "| Lint | − Skipped |\n| SAST | ✗ Fail |\n| Deps | Unconfirmed |\n\n" + failed,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}
			if err := appsummary.QualityCheckStatus(context.Background(), sink, appsummary.ParseQualityChecks(tc.records)); err != nil {
				t.Fatal(err)
			}

			if got := sink.buf.String(); got != tc.want {
				t.Errorf("summary =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}
