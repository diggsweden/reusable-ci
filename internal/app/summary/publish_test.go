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

	// Whole lines. "Release" on its own matched the prose in the closing
	// sentence, so the Type row -- the one thing that differs between a
	// release and a SNAPSHOT publish -- was never actually checked.
	got := sink.buf.String()
	for _, want := range []string{
		"## Published to Maven Central 🚀",
		"- **Version:** 1.2.3",
		"- **Type:** Release",
		"✓ **Release** deployed to staging. Will be published to Central within 30 minutes.\n",
		"*Published at 2026-05-10 14:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}

	if strings.Contains(got, "- **Type:** SNAPSHOT") {
		t.Errorf("unexpected SNAPSHOT type in %s", got)
	}

	if strings.Contains(got, "available immediately") {
		t.Fatalf("stable release claimed snapshot availability: %s", got)
	}
}

// TestMavenCentralPublish_SnapshotFlipsTypeAndAvailabilityNote is the other
// half of the IsSnapshot branch: a SNAPSHOT is available immediately, a
// release waits on Central's staging, and the summary has to say which.
func TestMavenCentralPublish_SnapshotFlipsTypeAndAvailabilityNote(t *testing.T) {
	t.Parallel()

	sink := &fakeSummarySink{}

	if err := appsummary.MavenCentralPublish(context.Background(), sink, appsummary.MavenCentralPublishInput{
		Version:    "1.2.3-SNAPSHOT",
		IsSnapshot: true,
		Now:        time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	got := sink.buf.String()
	for _, want := range []string{
		"- **Version:** 1.2.3-SNAPSHOT",
		"- **Type:** SNAPSHOT",
		"available immediately in the snapshot repository",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}

	if strings.Contains(got, "deployed to staging") {
		t.Errorf("SNAPSHOT should not claim a staging deployment: %s", got)
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

	// Whole lines: "npm" alone was already satisfied by the heading, so the
	// Package Type row went unchecked.
	got := sink.buf.String()
	for _, want := range []string{
		"## Published to GitHub Packages 📦",
		"- **Package Type:** npm",
		"- **Registry:** GitHub Packages",
		"- **Repository:** org/repo",
		"*Published at 2026-05-10 14:00:00 UTC*",
	} {
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

	// Both the heading and the Registry row take the label, and neither may
	// hard-code a forge: this block also renders for GitLab and Forgejo.
	got := sink.buf.String()
	for _, want := range []string{
		"## Published to the forge package registry 📦",
		"- **Registry:** the forge package registry",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("empty RegistryName should fall back to a forge-neutral label; missing %q in %s", want, got)
		}
	}

	if strings.Contains(got, "GitHub") {
		t.Errorf("forge-neutral fallback must not name a forge: %s", got)
	}
}
