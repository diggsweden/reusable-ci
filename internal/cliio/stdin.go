// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

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

func readStdin(stdin fs.File) ([]byte, error) {
	info, err := stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat stdin: %w", err)
	}

	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("%q expects piped or redirected input, not a terminal: %w", StdSentinel, errs.ErrUsage)
	}

	body, err := readBounded(stdin)
	if err != nil {
		return nil, fmt.Errorf("read from stdin: %w", err)
	}

	return body, nil
}
