// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import "os"

// StdinIsCharDevice reports whether os.Stdin is a character device
// (a TTY or /dev/null), as opposed to a pipe or regular file.
//
// The distinction with regular pipes is what the clig.dev "don't hang
// on a TTY" guard needs; the /dev/null case folds into the same warning
// since either way no meaningful input would be read.
func StdinIsCharDevice() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
