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

func TestGradleBuild_RendersAllFields(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.GradleBuild(context.Background(), sink, appsummary.GradleBuildInput{
		JavaVersion: "25",
		GradleTasks: "build :app:bundle",
		SkipTests:   true,
		Version:     "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Now:         time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{
		"## Gradle Build Summary 🔨",
		"- **Java:** 25",
		// Task paths go through summary.LiteralText like every other value
		// the build summaries render, so ":" is entity-encoded in the raw
		// output and displays as ":" once rendered. It used to be interpolated
		// raw, which let a newline in any field forge a summary row.
		"- **Tasks:** build &#58;app&#58;bundle",
		"- **Tests:** ⊘ Skipped",
		"- **Version:** 1.2.3",
		"*Build completed at 2026-05-10 12:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestGradleBuild_OmitsVersionWhenEmpty(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.GradleBuild(context.Background(), sink, appsummary.GradleBuildInput{
		JavaVersion: "21", GradleTasks: "build", SkipTests: false,
		Now: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(sink.buf.String(), "**Version:**") {
		t.Errorf("did not expect version line:\n%s", sink.buf.String())
	}
}
