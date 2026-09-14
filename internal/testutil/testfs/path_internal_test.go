// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package testfs

import (
	"fmt"
	"path/filepath"
	"testing"
)

// fatalProbe records a Fatalf instead of stopping the test.
type fatalProbe struct {
	testing.TB

	fatal string
}

func (p *fatalProbe) Helper() {}

func (p *fatalProbe) Fatalf(format string, args ...any) { p.fatal = fmt.Sprintf(format, args...) }

// TestRealPath_RefusesNamesOutsideTheRoot pins the helper's path policy: local
// names join below the owned root, while an absolute name, a parent step, a
// name that climbs after descending and an empty-component climb fail the test
// before any caller can write outside the root.
func TestRealPath_RefusesNamesOutsideTheRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	for _, tc := range []struct {
		parts  []string
		refuse bool
	}{
		{nil, false},
		{[]string{"a", "b.txt"}, false},
		{[]string{"a/../b.txt"}, false},
		{[]string{"/etc/passwd"}, true},
		{[]string{".."}, true},
		{[]string{"a", "..", ".."}, true},
		{[]string{"a/../../outside"}, true},
	} {
		probe := &fatalProbe{TB: t}
		fsys := &Real{t: probe, Root: root}

		got := fsys.Path(tc.parts...)
		if refused := probe.fatal != ""; refused != tc.refuse {
			t.Errorf("Path(%q) refused = %t (%q), want %t", tc.parts, refused, probe.fatal, tc.refuse)
		}

		if !tc.refuse {
			if rel, err := filepath.Rel(root, got); err != nil || (!filepath.IsLocal(rel) && rel != ".") {
				t.Errorf("Path(%q) = %q, not below %q", tc.parts, got, root)
			}
		}
	}
}
