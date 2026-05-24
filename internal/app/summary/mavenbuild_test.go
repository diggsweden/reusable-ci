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

func TestMavenBuild_RendersAllFields(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.MavenBuild(context.Background(), sink, appsummary.MavenBuildInput{
		BuildType:   "lib",
		GroupID:     "se.digg.example",
		ArtifactID:  "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Version:     "1.2.3-SNAPSHOT",
		JavaVersion: "25",
		SkipTests:   false,
		IsSnapshot:  true,
		Now:         time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{
		"## Maven Build Summary 🔨",
		"- **Type:** lib",
		"- **Artifact:** `se.digg.example:demo:1.2.3-SNAPSHOT`",
		"- **Java:** 25",
		"- **Tests:** ✓ Executed",
		"- **Snapshot:** true",
		"*Build completed at 2026-05-10 14:30:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMavenBuild_SkipTestsFlipsTestLine(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.MavenBuild(context.Background(), sink, appsummary.MavenBuildInput{
		BuildType: "app", GroupID: "g", ArtifactID: "a", Version: "1.0", JavaVersion: "21", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		SkipTests: true, IsSnapshot: false,
		Now: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	if !strings.Contains(got, "- **Tests:** ⊘ Skipped") {
		t.Errorf("missing skipped marker:\n%s", got)
	}

	if strings.Contains(got, "- **Tests:** ✓ Executed") {
		t.Errorf("unexpected executed marker:\n%s", got)
	}
}
