// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"fmt"
	"os"
)

// WithLock runs fn while holding an exclusive advisory lock on a sidecar
// "<path>.lock" file, serialising concurrent processes that read-modify-
// write path — e.g. parallel `ledger add` invocations against one ledger,
// which would otherwise clobber each other's writes and silently drop
// entries.
//
// The lock lives on the open file description (flock(2)), not the file's
// contents, so callers may freely read, truncate, and rewrite path inside
// fn. The sidecar lock file is created if absent and left in place.
//
// The directory holding path must already exist (the lock file is created
// beside it). On platforms without advisory file locking the lock is a
// best-effort no-op. fn's error is returned unchanged.
func WithLock(path string, fn func() error) error {
	lockPath := path + ".lock"

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644) //nolint:gosec,varnamelen // lock path derives from an operator-supplied path; 'f' is the idiomatic os.File name.
	if err != nil {
		return fmt.Errorf("open lock %s: %w", lockPath, err)
	}

	defer func() { _ = f.Close() }()

	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}

	defer func() { _ = unlock(f) }()

	return fn()
}
