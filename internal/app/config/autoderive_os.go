// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import "os"

// fileExistsOS is the os.Stat fallback used when no fs.FS is supplied.
// Lives in its own file so the AutoDeriveConfig surface is fs-agnostic
// for tests.
func fileExistsOS(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

func readFileOS(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // path is a manifest filename relative to a validated root.
}
