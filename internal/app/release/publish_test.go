// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

func TestPublishRelease_HappyPath(t *testing.T) {
	chdirTemp(t)
	mustWrite(t, "dist/release-notes.md", "release notes\n")
	mustWrite(t, "dist/asset.tgz", "asset\n")

	prov := fakeprovider.New(t)

	var out bytes.Buffer

	err := apprelease.PublishRelease(context.Background(), prov, &out, apprelease.PublishReleaseInput{
		Tag:              "v1.2.3",
		Repository:       "owner/repo",
		ReleaseName:      "repo v1.2.3",
		ReleaseNotesFile: "dist/release-notes.md",
		Draft:            true,
		Assets:           []string{"dist/asset.tgz"},
	})
	if err != nil {
		t.Fatal(err)
	}

	calls := prov.PublishReleaseCalls()
	if len(calls) != 1 {
		t.Fatalf("PublishRelease calls = %d", len(calls))
	}

	call := calls[0]
	if call.Repo != "owner/repo" {
		t.Errorf("Repo = %q", call.Repo)
	}

	if call.Spec.Tag != "v1.2.3" || call.Spec.Name != "repo v1.2.3" || call.Spec.NotesFile != "dist/release-notes.md" {
		t.Errorf("Spec = %+v", call.Spec)
	}

	if !call.Spec.Draft {
		t.Error("Draft should be true")
	}

	if got := strings.Join(call.Spec.Assets, ","); got != "dist/asset.tgz" {
		t.Errorf("Assets = %q", got)
	}

	if !strings.Contains(out.String(), "Publishing release v1.2.3 with 1 asset(s)") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

func TestPublishRelease_RejectsDuplicateAssetBasenamesBeforeProviderCall(t *testing.T) {
	chdirTemp(t)
	mustWrite(t, "dist/release-notes.md", "release notes\n")
	mustWrite(t, "dist/linux/asset.tgz", "linux\n")
	mustWrite(t, "dist/darwin/asset.tgz", "darwin\n")

	prov := fakeprovider.New(t)

	err := apprelease.PublishRelease(context.Background(), prov, &bytes.Buffer{}, apprelease.PublishReleaseInput{
		Tag:              "v1.2.3",
		Repository:       "owner/repo",
		ReleaseNotesFile: "dist/release-notes.md",
		Assets:           []string{"dist/linux/asset.tgz", "dist/darwin/asset.tgz"},
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "duplicate release asset basename") {
		t.Fatalf("err = %v, want duplicate basename validation", err)
	}

	if calls := prov.PublishReleaseCalls(); len(calls) != 0 {
		t.Fatalf("provider was called despite invalid assets: %+v", calls)
	}
}

func TestPublishRelease_RejectsUnsafeOrMissingInputs(t *testing.T) {
	chdirTemp(t)
	mustWrite(t, "dist/release-notes.md", "release notes\n")
	mustWrite(t, "dist/asset.tgz", "asset\n")

	if err := os.Symlink("release-notes.md", "dist/notes-link.md"); err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		in      apprelease.PublishReleaseInput
		wantErr error
	}{
		"missing tag": {
			in:      apprelease.PublishReleaseInput{Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrUsage,
		},
		"unsafe notes": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "../release-notes.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrValidation,
		},
		"notes symlink": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/notes-link.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrMissingInput,
		},
		"unsafe asset": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"/tmp/asset.tgz"}},
			wantErr: errs.ErrValidation,
		},
		"missing asset": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"dist/missing.tgz"}},
			wantErr: errs.ErrMissingInput,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			prov := fakeprovider.New(t)

			err := apprelease.PublishRelease(context.Background(), prov, &bytes.Buffer{}, tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			if calls := prov.PublishReleaseCalls(); len(calls) != 0 {
				t.Fatalf("provider was called despite invalid input: %+v", calls)
			}
		})
	}
}

func chdirTemp(t *testing.T) {
	t.Helper()

	t.Chdir(t.TempDir())
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
