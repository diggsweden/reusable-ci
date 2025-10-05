// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"strings"
	"testing"
)

// TestPorcelainCommandsResolve guards the curated COMMON COMMANDS block in the
// root help against drift: every path advertised to humans must still resolve
// to a real command in the tree. A rename that misses this list fails here.
func TestPorcelainCommandsResolve(t *testing.T) {
	t.Parallel()

	root := New(BuildInfo{})

	for _, pc := range porcelainCommands {
		cur := root
		for _, seg := range strings.Fields(pc.path) {
			next := cur.Command(seg)
			if next == nil {
				t.Fatalf("porcelain command %q: segment %q does not resolve to a command", pc.path, seg)
			}

			cur = next
		}
	}
}
