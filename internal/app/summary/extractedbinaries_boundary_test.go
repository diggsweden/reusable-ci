// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
)

//nolint:gocognit // Eight boundary cases share one fixture and complete report/call oracle.
func TestExtractedBinaries_FileListBoundaries(t *testing.T) {
	const metadata = "### Extracted Binaries - boundary-demo (linux/amd64)\n\n" +
		"- **Stage:** export-binary\n- **Artifact:** boundary-artifact\n" +
		"- **Platform:** linux/amd64\n- **Binaries:** metadata-only-name\n" +
		"\nFiles in extracted-binaries/:\n\n"
	// Independent default oracle: neither the fixture count nor production's limit determines it.
	const first50 = "`f00`\n`f01`\n`f02`\n`f03`\n`f04`\n`f05`\n`f06`\n`f07`\n`f08`\n`f09`\n" +
		"`f10`\n`f11`\n`f12`\n`f13`\n`f14`\n`f15`\n`f16`\n`f17`\n`f18`\n`f19`\n" +
		"`f20`\n`f21`\n`f22`\n`f23`\n`f24`\n`f25`\n`f26`\n`f27`\n`f28`\n`f29`\n" +
		"`f30`\n`f31`\n`f32`\n`f33`\n`f34`\n`f35`\n`f36`\n`f37`\n`f38`\n`f39`\n" +
		"`f40`\n`f41`\n`f42`\n`f43`\n`f44`\n`f45`\n`f46`\n`f47`\n`f48`\n`f49`\n"
	// Only this known fixture's separators differ; do not use the production encoder as an oracle.
	nestedLine := "<code>bin/deep/tool&#96;</code>\n"
	if os.PathSeparator == '\\' {
		nestedLine = "<code>bin&#92;deep&#92;tool&#96;</code>\n"
	}

	sentinel := errors.New("extracted binaries append sentinel") //nolint:err113 // Injected error identity, not a production error.
	for _, tc := range []struct {
		name      string
		limit     int
		fileCount int
		files     []string
		missing   bool
		sinkError error
		wantFiles string
	}{
		{
			name: "explicit_limit_at_boundary", limit: 2,
			files: []string{"bin/deep/tool`", "bin.meta"}, wantFiles: "`bin.meta`\n" + nestedLine,
		},
		{
			name: "explicit_limit_above_boundary", limit: 2,
			files: []string{"bin/deep/tool`", "bin.meta", "a-first"}, wantFiles: "`a-first`\n`bin.meta`\n",
		},
		{name: "default_limit_at_boundary", limit: 0, fileCount: 50, wantFiles: first50},
		{name: "default_limit_above_boundary", limit: 0, fileCount: 51, wantFiles: first50},
		{name: "negative_limit_uses_default", limit: -1, fileCount: 51, wantFiles: first50},
		{name: "positive_limit_above_default", limit: 51, fileCount: 51, wantFiles: first50 + "`f50`\n"},
		{name: "missing_directory", missing: true},
		{
			name: "sink_failure_nonempty", limit: 2, sinkError: sentinel,
			files: []string{"bin/deep/tool`", "bin.meta"}, wantFiles: "`bin.meta`\n" + nestedLine,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Chdir and its cleanup must stay serial, including the parent test.
			t.Chdir(t.TempDir())

			dir := "."
			if tc.missing {
				dir = "missing-child"
				if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("fixture must be absent before extraction: %v", err)
				}
			} else {
				// WalkDir visits this empty directory first; directories must not consume the limit.
				if err := os.Mkdir("00-empty", 0o700); err != nil {
					t.Fatal(err)
				}
			}

			for _, name := range tc.files {
				path := filepath.FromSlash(name)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(path, []byte("boundary fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Reverse creation order must not change the complete ascending report.
			for i := tc.fileCount - 1; i >= 0; i-- {
				if err := os.WriteFile(fmt.Sprintf("f%02d", i), []byte("boundary fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			ctx := t.Context()
			calls := 0
			want := metadata + tc.wantFiles
			sink := appendFailureSink(func(gotCtx context.Context, markdown string) error {
				calls++

				if gotCtx != ctx {
					t.Error("Append did not receive the caller's exact context")
				}

				if markdown != want {
					t.Errorf("summary = %q, want %q", markdown, want)
				}

				return tc.sinkError
			})

			err := appsummary.ExtractedBinaries(ctx, sink, appsummary.ExtractedBinariesInput{
				Dir: dir, DisplayName: "boundary-demo", Platform: "linux/amd64",
				ExtractTarget: "export-binary", ArtifactName: "boundary-artifact",
				ExpectedNames: "metadata-only-name", Limit: tc.limit,
			})
			if err != tc.sinkError { //nolint:err113,errorlint // Require the exact sentinel, not a wrapper or same-text replacement.
				t.Errorf("append error = %v, want unchanged error %v", err, tc.sinkError)
			}

			if calls != 1 {
				t.Errorf("Append calls = %d, want exactly one", calls)
			}

			if tc.missing {
				if _, statErr := os.Lstat(dir); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("missing directory changed after extraction: %v", statErr)
				}
			}
		})
	}
}
