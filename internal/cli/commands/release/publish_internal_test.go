// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// TestDryRunReleaseForge_PublishReleaseNarratesUploads exercises the
// reconcile-strategy preview: the decorator narrates the create/update call
// and every asset upload (with an on-disk existence check) and performs no
// forge mutation — by construction it holds no forge client at all.
func TestDryRunReleaseForge_PublishReleaseNarratesUploads(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existing := filepath.Join(dir, "asset.tgz")

	if err := os.WriteFile(existing, []byte("asset\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(dir, "missing.tgz")

	var buf bytes.Buffer

	err := dryRunReleaseForge{out: &buf}.PublishRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:    "v1.2.3",
		Name:   "repo v1.2.3",
		Draft:  true,
		Assets: []string{existing, missing},
	})
	if err != nil {
		t.Fatalf("PublishRelease: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		`[dry-run] would create or update release v1.2.3 ("repo v1.2.3", draft=true, prerelease=false) on owner/repo`,
		"[dry-run] would upload asset " + existing + "\n",
		"[dry-run] would upload asset " + missing + " (missing on disk)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing narration %q", out, want)
		}
	}
}

// TestDryRunReleaseForge_CreateReleaseNarratesDeleteAndCreate exercises the
// recreate-strategy preview: delete-then-create is narrated per API call.
func TestDryRunReleaseForge_CreateReleaseNarratesDeleteAndCreate(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := dryRunReleaseForge{out: &buf}.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.2.3",
		Name:       "v1.2.3",
		Draft:      true,
		MakeLatest: provider.MakeLatestTrue,
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"[dry-run] would delete any existing release for tag v1.2.3 on owner/repo",
		`[dry-run] would create release v1.2.3 ("v1.2.3", draft=true, prerelease=false, make-latest=true) on owner/repo`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing narration %q", out, want)
		}
	}
}

// TestPublishCmd_HasDryRunFlag is the guardrail: `release publish` mutates
// the forge and must expose --dry-run so operators can preview first.
func TestPublishCmd_HasDryRunFlag(t *testing.T) {
	t.Parallel()

	for _, flag := range publishCmd().Flags {
		if slices.Contains(flag.Names(), "dry-run") {
			return
		}
	}

	t.Error("release publish must expose --dry-run")
}
