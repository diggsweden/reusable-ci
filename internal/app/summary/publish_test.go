// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func TestMavenCentralPublish_RendersSummary(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.MavenCentralPublish(context.Background(), sink, appsummary.MavenCentralPublishInput{Version: "1.2.3", Now: time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{"Published to Maven Central", "1.2.3", "Release", "2026-05-10 14:00:00 UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestGitHubPackagesPublish_RendersSummary(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.GitHubPackagesPublish(context.Background(), sink, appsummary.GitHubPackagesPublishInput{Repository: "org/repo", PackageType: "npm", Now: time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{"Published to GitHub Packages", "npm", "org/repo", "2026-05-10 14:00:00 UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
