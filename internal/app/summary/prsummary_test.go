// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

func fixedNow() time.Time {
	return time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC)
}

func TestPRSummary_HappyPath(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.PRSummary(context.Background(), sink, appsummary.PRSummaryInput{
		ProjectType: "npm", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Branch:      "feat/foo",
		Commit:      "abcdef0123456789",
		Actor:       "alice",
		RunURL:      "https://example.com/run/1", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		QualityStageResultJSON: stageResultJSON(t, "pr-quality", map[string]string{
			"nanolinter": "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			"swift":      "skipped",
		}),
		Now: fixedNow(),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"# Pull Request Summary",
		"| **Project Type** | `npm` |",
		"| **Branch** | `feat/foo` |",
		"| **Commit** | `abcdef0` |", // truncated
		"| **Checked By** | @alice |",
		"| **Checked At** | 2026-05-10 14:30:00 UTC |",
		"| Nanolinter | ✓ |",
		"| Swift Lint | − |",
		"- [Workflow Run](https://example.com/run/1)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\nfull:\n%s", want, body)
		}
	}
}

func TestPRSummary_MissingTargetsDefaultToSkipped(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.PRSummary(context.Background(), sink, appsummary.PRSummaryInput{
		ProjectType: "go",
		Branch:      "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Commit:      "abcdef0",
		Actor:       "bot", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		// Empty JSON → all targets surface as "skipped".
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	if strings.Count(body, "| ✗ |") != 0 {
		t.Errorf("no failures expected on empty input: %s", body)
	}
}

func TestPRSummary_FailureAndSkippedIcons(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.PRSummary(context.Background(), sink, appsummary.PRSummaryInput{
		ProjectType: "maven", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Branch:      "feat/my-branch",
		Commit:      "abc1234567890",
		Actor:       "test-user",
		QualityStageResultJSON: stageResultJSON(t, "pr-quality", map[string]string{
			"nanolinter": "failure",
			"swift":      "skipped",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := sink.buf.String()
	for _, want := range []string{"| Nanolinter | ✗ |", "| Swift Lint | − |"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestPRSummary_RejectsMalformedStageResultJSON(t *testing.T) {
	t.Parallel()

	err := appsummary.PRSummary(context.Background(), &fakeSummarySink{}, appsummary.PRSummaryInput{
		QualityStageResultJSON: `{"stage":"pr-quality","targets":{}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("err = %v", err)
	}
}
