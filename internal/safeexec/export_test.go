// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build linux

package safeexec

import "testing"

// UseProcSwapsFixture points the kernel swap reader at path for one test, so
// tests can exercise kernel semantics without the reported override.
func UseProcSwapsFixture(t *testing.T, path string) {
	t.Helper()

	previous := procSwapsPath
	procSwapsPath = path

	t.Cleanup(func() { procSwapsPath = previous })
}
