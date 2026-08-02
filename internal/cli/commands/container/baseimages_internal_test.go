// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import "testing"

func TestBaseImagesGroupExposesWorkflowCommands(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, cmd := range baseImagesGroup().Commands {
		found[cmd.Name] = true
	}

	for _, name := range []string{"collect", "input-field", "arch-ref", "arch-metadata", "candidate-metadata", "sign", "verify-existing", "promote"} {
		if !found[name] {
			t.Errorf("base-images command %q not exposed", name)
		}
	}
}
