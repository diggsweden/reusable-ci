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

// TestPublishRelease_PassesEveryFieldThroughToTheProvider checks that what the
// caller asked for is what the provider is told: tag, name, notes file, draft
// flag and assets all arrive in one call, and the operator sees a line saying
// what is being published.
func TestPublishRelease_PassesEveryFieldThroughToTheProvider(t *testing.T) {
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

// TestPublishRelease_RefusesBadInputWithoutCallingTheProvider covers each way
// an input can be refused — a missing tag, a path that climbs out of the
// working directory, an absolute path, a symlink standing in for a file, a
// file that is not there. What every row shares is the second assertion: the
// provider is never reached. Refusing late, after the release exists upstream,
// would not be a refusal.
func TestPublishRelease_RefusesBadInputWithoutCallingTheProvider(t *testing.T) {
	chdirTemp(t)
	mustWrite(t, "dist/release-notes.md", "release notes\n")
	mustWrite(t, "dist/asset.tgz", "asset\n")

	if err := os.Symlink("release-notes.md", "dist/notes-link.md"); err != nil {
		t.Fatal(err)
	}

	// An absolute path to a file that is real, valid and inside this test's
	// own directory. Being absolute is then the only thing left to refuse it
	// for. The row used to name /tmp/asset.tgz, which was safe only because
	// the absolute-path check happens to run before anything opens the file.
	sandbox, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	absoluteAsset := filepath.Join(sandbox, "dist", "asset.tgz")

	tests := map[string]struct {
		in      apprelease.PublishReleaseInput
		wantErr error
	}{
		"missing tag": {
			in:      apprelease.PublishReleaseInput{Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrUsage,
		},
		"notes path climbs out of the working directory": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "../release-notes.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrValidation,
		},
		"notes are a symlink": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/notes-link.md", Assets: []string{"dist/asset.tgz"}},
			wantErr: errs.ErrMissingInput,
		},
		"asset path is absolute": {
			in:      apprelease.PublishReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", ReleaseNotesFile: "dist/release-notes.md", Assets: []string{absoluteAsset}},
			wantErr: errs.ErrValidation,
		},
		"asset is not there": {
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
