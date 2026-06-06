// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
)

func TestGoBuild_RendersSummary(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.GoBuild(context.Background(), sink, appsummary.GoBuildInput{BinaryName: "app", Module: "github.com/org/app", Platforms: "linux/amd64", Version: "1.2.3", SkipTests: true}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{"Go Build Summary", "github.com/org/app", "app", "skipped"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
