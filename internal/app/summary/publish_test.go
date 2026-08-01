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

func TestForgePackagesPublish_RendersSummary(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	// RegistryName is forge-resolved by the caller (GitHub Packages / GitLab
	// Package Registry / …); the block must render whatever it's given.
	if err := appsummary.ForgePackagesPublish(context.Background(), sink, appsummary.ForgePackagesPublishInput{Repository: "org/repo", PackageType: "npm", RegistryName: "GitHub Packages", Now: time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{"Published to GitHub Packages", "npm", "org/repo", "2026-05-10 14:00:00 UTC"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestForgePackagesPublish_FallsBackToNeutralLabel(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}
	if err := appsummary.ForgePackagesPublish(context.Background(), sink, appsummary.ForgePackagesPublishInput{Repository: "org/repo", PackageType: "maven"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.buf.String(); !strings.Contains(got, "the forge package registry") {
		t.Errorf("empty RegistryName should fall back to a forge-neutral label; got %s", got)
	}
}
