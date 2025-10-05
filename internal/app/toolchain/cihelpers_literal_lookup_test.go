// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMiseToolBinary_LiteralRootAndOrderedChildren(t *testing.T) {
	root := changelogFixtureRoot(t)
	installDir := filepath.Join(root, "tool[1]")
	firstChild := filepath.Join(installDir, "a", gitCliffBin)
	direct := filepath.Join(installDir, gitCliffBin)
	writeChangelogCanary(t, filepath.Join(root, "tool1", "a", gitCliffBin), "decoy executable, never run\n", 0o751)
	writeChangelogCanary(t, filepath.Join(installDir, "z", gitCliffBin), "later child, never run\n", 0o751)
	linkedDir := filepath.Join(root, "linked-child")
	writeChangelogCanary(t, filepath.Join(linkedDir, "source"), "selected child, never run\n", 0o751)
	changelogSymlink(t, "source", filepath.Join(linkedDir, gitCliffBin))
	changelogSymlink(t, linkedDir, filepath.Join(installDir, "a"))

	for _, scenario := range []string{"ordered children", "direct first"} {
		t.Run(scenario, func(t *testing.T) {
			want, body := firstChild, "selected child, never run\n"
			if scenario == "direct first" {
				want, body = direct, "direct executable, never run\n"
				writeChangelogCanary(t, direct, body, 0o751)
			}

			unchanged := changelogUnchanged(t, root)

			got, err := findMiseToolBinary(installDir, gitCliffBin)
			if err != nil || got != want {
				t.Errorf("literal install root selection = (%q, %v), want %q", got, err, want)
			}

			if bytes, readErr := os.ReadFile(got); readErr != nil || string(bytes) != body {
				t.Errorf("selected inert fixture = %q, want %q: %v", bytes, body, readErr)
			}

			unchanged()
		})
	}
}
