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
	"github.com/stretchr/testify/require"
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
	asset := filepath.Join(t.TempDir(), "asset.tgz")
	require.NoError(t, os.WriteFile(asset, []byte("owned"), 0o600))

	var buf bytes.Buffer

	err := dryRunReleaseForge{out: &buf}.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.2.3",
		Name:       "v1.2.3",
		Draft:      true,
		MakeLatest: provider.MakeLatestTrue,
		Assets:     []string{asset},
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	require.Equal(t, "[dry-run] would delete any existing release for tag v1.2.3 on owner/repo\n"+
		"[dry-run] would create release v1.2.3 (\"v1.2.3\", draft=true, prerelease=false, make-latest=true) on owner/repo\n"+
		"[dry-run] would upload asset "+asset+"\n", buf.String())
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

// TestDryRunReleaseForge_JoinsFilesystemFactsToTheNarration covers the
// annotation branch, which every existing case walks past.
//
// A dry-run exists so an operator can see what a release WOULD do before it
// does it, and the assets are checked on disk while the narration is produced —
// so the two are one trace rather than a story told beside the facts. The
// existing test passes an asset that exists, so the "(missing on disk)" branch
// never fires: a dry-run that cheerfully reports it would upload a file that is
// not there reads as a green preview of a release that will fail, which is
// worse than no preview.
//
// The order matters as much as the content. Deletion is narrated before
// creation and creation before the uploads, because that is the sequence the
// real run performs, and a preview that reordered them would describe a
// different operation.
func TestDryRunReleaseForge_JoinsFilesystemFactsToTheNarration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	present := filepath.Join(dir, "present.tgz")
	require.NoError(t, os.WriteFile(present, []byte("owned"), 0o600))

	// A directory is not an uploadable asset even though it exists, so the
	// check is about the file TYPE and not merely about presence.
	directory := filepath.Join(dir, "a-directory")
	require.NoError(t, os.Mkdir(directory, 0o750))

	absent := filepath.Join(dir, "absent.tgz")

	var buf bytes.Buffer

	err := dryRunReleaseForge{out: &buf}.CreateRelease(context.Background(), "owner/repo", provider.ReleaseSpec{
		Tag:        "v1.2.3",
		Name:       "v1.2.3",
		MakeLatest: provider.MakeLatestTrue,
		Assets:     []string{present, absent, directory},
	})
	require.NoError(t, err)

	require.Equal(t,
		"[dry-run] would delete any existing release for tag v1.2.3 on owner/repo\n"+
			"[dry-run] would create release v1.2.3 (\"v1.2.3\", draft=false, prerelease=false, make-latest=true) on owner/repo\n"+
			"[dry-run] would upload asset "+present+"\n"+
			"[dry-run] would upload asset "+absent+" (missing on disk)\n"+
			"[dry-run] would upload asset "+directory+" (missing on disk)\n",
		buf.String())
}

// TestDryRunReleaseForge_MutatesNothing is the other half of a dry run: it
// narrates and touches no filesystem state of its own. A preview that created
// or removed anything would not be a preview.
func TestDryRunReleaseForge_MutatesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	asset := filepath.Join(dir, "asset.tgz")
	require.NoError(t, os.WriteFile(asset, []byte("owned"), 0o600))

	before, err := os.ReadFile(asset)
	require.NoError(t, err)

	require.NoError(t, dryRunReleaseForge{out: &bytes.Buffer{}}.CreateRelease(
		context.Background(), "owner/repo", provider.ReleaseSpec{
			Tag: "v1.2.3", MakeLatest: provider.MakeLatestTrue, Assets: []string{asset},
		}))

	after, err := os.ReadFile(asset)
	require.NoError(t, err)
	require.Equal(t, before, after, "the dry run modified the asset")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the dry run created or removed files")
}
