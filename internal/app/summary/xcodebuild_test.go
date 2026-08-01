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

func TestXcodeBuild_Signed(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	err := appsummary.XcodeBuild(context.Background(), sink, appsummary.XcodeBuildInput{
		XcodeVersion: "16.4",
		Scheme:       "App",
		Signing:      true,
		Version:      "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		BuildNumber:  "42",
		IPAName:      "demo-1.2.3",
		Now:          time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{
		"## Xcode Build Summary 📱",
		"| **Xcode** | 16.4 |",
		"| **Scheme** | App |",
		"| **Configuration** | Release |",            // default
		"| **Destination** | generic/platform=iOS |", // default
		"| **Signing** | ✓ Enabled |",
		"| **Version** | 1.2.3 (42) |",
		"✓ IPA: `demo-1.2.3`",
		"*Build completed at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestXcodeBuild_UnsignedFallsBackToArchive(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.XcodeBuild(context.Background(), sink, appsummary.XcodeBuildInput{
		XcodeVersion: "16.4", Scheme: "App", Signing: false, IPAName: "demo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Now: time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "✓ Archive: `demo-archive`") {
		t.Errorf("missing archive marker:\n%s", sink.buf.String())
	}
}
