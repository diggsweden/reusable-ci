// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestFindArtifactsByExts_ReturnsEveryMatchInPathOrder pins the complete list
// the selection is taken from. The public tests see one selected file, and
// each fixture held one or two candidates, so a scan that dropped a match,
// admitted a distractor, or returned the walk's order instead of sorted paths
// selected the same file.
//
// The fixture has a match beside a directory of the same stem (b.aab and
// b/c.aab), where directory-walk order and sorted path order differ; an
// extension in upper case; a name that only contains the extension; a
// directory named like an artifact; another extension; and a sibling
// directory whose name starts with the scanned one.
func TestFindArtifactsByExts_ReturnsEveryMatchInPathOrder(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	dir := filepath.Join(base, "artifacts")

	for _, name := range []string{
		"b/c.aab",
		"b.aab",
		"A-debug.AAB",
		"notes.aab.txt",
		"aab",
		"x.aab/inner.txt",
		"app.apk",
		"../artifacts-old/stale.aab",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	in := func(names ...string) []string {
		out := make([]string, 0, len(names))
		for _, name := range names {
			out = append(out, filepath.Join(dir, name))
		}

		return out
	}

	for name, tc := range map[string]struct {
		exts      []string
		recursive bool
		want      []string
	}{
		"recursive":             {exts: []string{".aab"}, recursive: true, want: in("A-debug.AAB", "b.aab", "b/c.aab")},
		"top level only":        {exts: []string{".aab"}, want: in("A-debug.AAB", "b.aab")},
		"either extension":      {exts: []string{".apk", ".aab"}, recursive: true, want: in("A-debug.AAB", "app.apk", "b.aab", "b/c.aab")},
		"extension nobody used": {exts: []string{".ipa"}, recursive: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := findArtifactsByExts(dir, tc.exts, tc.recursive)
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("matches =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
