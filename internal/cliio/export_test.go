// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"io"
	"os"
)

// WriteLinesAndCloseForTest exposes the append path's write-and-close seam so a
// close failure can be injected. A real file's flush cannot be made to fail
// portably, and that is exactly the failure worth checking.
func WriteLinesAndCloseForTest(dst io.WriteCloser, lines []string) error {
	return writeLinesAndClose(dst, lines)
}

// WithLockUsingForTest exposes the lock orchestration with its acquire and
// release primitives injectable, so an acquisition failure can be forced
// through the real code path. flock on a valid descriptor cannot be made to
// fail on demand, and what matters is not flock's error but what WithLock does
// with it.
func WithLockUsingForTest(path string, acquire, release func(*os.File) error, fn func() error) error {
	return withLockUsing(path, acquire, release, fn)
}
