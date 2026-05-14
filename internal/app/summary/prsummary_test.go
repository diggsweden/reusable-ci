// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func fixedNow() time.Time {
	return time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC)
}

func TestPRSummary_HappyPath(t *testing.T) {
	t.Parallel()
	sink := &fakeSummarySink{}
	err := appsummary.PRSummary(context.Background(), sink, appsummary.PRSummaryInput{
		ProjectType: "npm",
		Branch:      "feat/foo",
		Commit:      "abcdef0123456789",
		Actor:       "alice",
		RunURL:      "https://example.com/run/1",
		QualityStageResultJSON: `{"targets":{"dependencyreview":"success","sastopengrep":"failure",
			"publiccodelint":"skipped","devbasecheck":"success","swift":"skipped"}}`,
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
		"| Devbase Check | ✓ |",
		"| Dependency Review | ✓ |",
		"| OpenGrep SAST | ✗ |",
		"| Publiccode Lint | − |",
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
		Branch:      "main",
		Commit:      "abcdef0",
		Actor:       "bot",
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
		ProjectType: "maven",
		Branch:      "feat/my-branch",
		Commit:      "abc1234567890",
		Actor:       "test-user",
		QualityStageResultJSON: `{"targets":{"dependencyreview":"skipped","sastopengrep":"failure","publiccodelint":"success","devbasecheck":"failure","swift":"skipped"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := sink.buf.String()
	for _, want := range []string{"| Devbase Check | ✗ |", "| OpenGrep SAST | ✗ |", "| Dependency Review | − |"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}
