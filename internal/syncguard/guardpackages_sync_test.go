// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package syncguard

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
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

	entries := reporoot.ReadDir(t, "internal")
	table := string(reporoot.ReadFile(t, "docs/testing.md"))
	rows, err := guardPackagesNamedIn(table)
	require.NoError(t, err)

	var found []string

	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "guard") {
			continue
		}

		found = append(found, e.Name())

		// The table cites each package as `internal/<name>` in a row.
		if !slices.Contains(rows, e.Name()) {
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
	for _, name := range rows {
		if !slices.Contains(found, name) {
			t.Errorf(
				"docs/testing.md has a row for internal/%s, which does not exist.\n"+
					"    Remove the row, or restore the package.",
				name,
			)
		}
	}
}

// guardPackagesNamedIn accepts complete rows only in the exact guard table;
// prose, unrelated tables, duplicate rows and missing columns cannot satisfy it.
func guardPackagesNamedIn(doc string) ([]string, error) {
	var out []string

	active, found := false, false

	for index, line := range strings.Split(doc, "\n") {
		if line == "| Package | Guards | Reads |" {
			if found {
				return nil, fmt.Errorf("duplicate guard table at line %d: %w", index+1, errs.ErrValidation)
			}

			active, found = true, true

			continue
		}

		if !active {
			continue
		}

		if !strings.HasPrefix(line, "|") {
			active = false

			continue
		}

		cells := strings.Split(line, "|")
		if len(cells) != 5 {
			return nil, fmt.Errorf("guard table line %d needs three columns: %w", index+1, errs.ErrValidation)
		}

		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}

		if strings.Trim(cells[1], "-:") == "" {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(cells[1], "`internal/"), "`")
		if cells[1] != "`internal/"+name+"`" || !isPackageIdent(name) || !strings.HasSuffix(name, "guard") || cells[2] == "" || cells[3] == "" || slices.Contains(out, name) {
			return nil, fmt.Errorf("invalid or duplicate guard row at line %d: %w", index+1, errs.ErrValidation)
		}

		out = append(out, name)
	}

	if !found || len(out) == 0 {
		return nil, fmt.Errorf("guard-package table is missing or empty: %w", errs.ErrValidation)
	}

	return out, nil
}

// isPackageIdent reports whether s could be a Go package directory name, which
// excludes the glob the prose uses for the family as a whole.
func isPackageIdent(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_", r) {
			return false
		}
	}

	return true
}
