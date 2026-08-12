// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
)

// TestGuardPackagesAreDocumented fails when a repo-wide guard package is
// missing from the table in docs/testing.md, or the table names one that no
// longer exists.
//
// The four guard packages were briefly described in six places at once: this
// table, an ADR, and each package's own doc comment listing its siblings.
// Nothing compared them, so adding a fifth package meant remembering six
// edits. The table is now the only enumeration, and this is what keeps it
// honest -- the package docs point here instead of repeating each other.
//
// It checks membership, not wording: the prose describing what each guard
// covers is not something a test can judge.
func TestGuardPackagesAreDocumented(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)

	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("read internal/: %v", err)
	}

	doc, err := os.ReadFile(filepath.Join(root, "docs", "testing.md")) //nolint:gosec // repo-local doc.
	if err != nil {
		t.Fatalf("read docs/testing.md: %v", err)
	}

	table := string(doc)

	var found []string

	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "guard") {
			continue
		}

		found = append(found, e.Name())

		// The table cites each package as `internal/<name>` in a row.
		if !strings.Contains(table, "`internal/"+e.Name()+"`") {
			t.Errorf(
				"internal/%s is a repo-wide guard package with no row in docs/testing.md.\n"+
					"    Add one saying what it guards and what it reads. That table is the\n"+
					"    single list of guard packages -- the package doc comments point at it\n"+
					"    rather than enumerating each other, so this is the only place to edit.",
				e.Name(),
			)
		}
	}

	if len(found) == 0 {
		t.Fatal("no internal/*guard packages found; this guard would pass vacuously")
	}

	// The reverse direction: a row naming a package that has been removed.
	for _, name := range guardPackagesNamedIn(table) {
		if !containsString(found, name) {
			t.Errorf(
				"docs/testing.md has a row for internal/%s, which does not exist.\n"+
					"    Remove the row, or restore the package.",
				name,
			)
		}
	}
}

// guardPackagesNamedIn extracts every `internal/<something>guard` the document
// cites in backticks.
//
// Only lower-case identifiers count, so the prose may still write the glob
// `internal/*guard` when describing the family without it being read as a
// package that ought to exist.
func guardPackagesNamedIn(doc string) []string {
	var out []string

	for rest := doc; ; {
		const marker = "`internal/"

		i := strings.Index(rest, marker)
		if i < 0 {
			return out
		}

		rest = rest[i+len(marker):]

		end := strings.IndexByte(rest, '`')
		if end < 0 {
			return out
		}

		if name := rest[:end]; isPackageIdent(name) && strings.HasSuffix(name, "guard") && !containsString(out, name) {
			out = append(out, name)
		}

		rest = rest[end:]
	}
}

// isPackageIdent reports whether s could be a Go package directory name, which
// excludes the glob the prose uses for the family as a whole.
func isPackageIdent(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}

	return true
}

func containsString(haystack []string, want string) bool {
	for _, s := range haystack {
		if s == want {
			return true
		}
	}

	return false
}
