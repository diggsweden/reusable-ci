// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
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

func TestParseQualityChecks(t *testing.T) {
	t.Parallel()

	got := appsummary.ParseQualityChecks([]string{
		"Nanolinter|true|success",
		"Sast|false|skipped",
		"malformed-no-pipes",
	})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (malformed line skipped)", len(got))
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

func TestQualityCheckStatus_NamesWithSpacesAndSpecialChars(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.QualityCheckStatus(context.Background(), sink, []appsummary.QualityCheck{
		{Name: "Shell Check", Enabled: true, Result: domainsummary.ResultSuccess},
		{Name: "check-yaml", Enabled: true, Result: domainsummary.ResultSuccess},
	}); err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{"Shell Check", "check-yaml"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}
