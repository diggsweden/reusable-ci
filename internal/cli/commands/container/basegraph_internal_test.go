// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import "testing"

func TestBaseGraphGroupExposesWorkflowCommands(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, cmd := range baseGraphGroup().Commands {
		found[cmd.Name] = true
	}

	for _, name := range []string{"inputs", "input-set-id", "flavor", "groups-json", "group-flavors", "group-context-files", "groups-for-missing"} {
		if !found[name] {
			t.Errorf("base-graph command %q not exposed", name)
		}
	}
}
