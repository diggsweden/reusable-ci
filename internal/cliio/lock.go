// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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
// beside it). fn's error is returned unchanged. Explicit unlock is
// best-effort; the deferred close also releases the lock. Symlink paths and
// multiply-linked targets or sidecars are refused: atomic pathname replacement
// cannot provide one stable lock identity for such aliases.
//
// WithLock is Unix-only, like the release targets (linux and darwin). There
// is deliberately no no-op fallback: a lock that silently does not lock would
// run the read-modify-write it exists to serialise.
//
//nolint:cyclop // validates target and sidecar aliases, then descriptor identity before locking.
func WithLock(path string, fn func() error) error {
	return withLockUsing(path, lockExclusive, unlock, fn)
}

// withLockUsing is WithLock with the acquire and release primitives passed
// in, so a test can force an acquisition failure through the real
// orchestration instead of calling the primitive directly.
//
// flock on a valid descriptor does not fail on demand, and the failure that
// matters is not what flock returns — it is what WithLock does next. A run
// that could not take the lock must not run the callback anyway: the callback
// is a read-modify-write of the file the lock exists to serialise, and running
// it unserialised is the corruption the whole mechanism prevents.
func withLockUsing(path string, acquire, release func(*os.File) error, fn func() error) error {
	lockPath := path + ".lock"

	root, err := pathsafe.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	if err = refuseAliasedLockPaths(root, filepath.Base(lockPath)); err != nil {
		return err
	}

	lockFile, err := openVerifiedLockFile(root, filepath.Base(lockPath), lockPath)
	if err != nil {
		return err
	}

	defer func() { _ = lockFile.Close() }()

	if err := acquire(lockFile); err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}

	defer func() { _ = release(lockFile) }()

	// The target is inspected only once the lock is held. A holder replaces
	// the target with an atomic rename inside fn, and a waiter that looked
	// before acquiring could catch the outgoing inode between the rename and
	// its own stat, with a link count of zero, and refuse a file nobody had
	// aliased. Under the lock the target is quiescent.
	if err := refuseAliasedLockPaths(root, filepath.Base(path)); err != nil {
		return err
	}

	return fn()
}

// refuseAliasedLockPaths rejects a target or lock path that is not a plain,
// single-linked regular file. A hardlink or a symlink means the bytes the lock
// protects are reachable under another name, so serialising on this one
// protects nothing.
func refuseAliasedLockPaths(root *os.Root, names ...string) error {
	for _, name := range names {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() || !singleLink(info) {
			return fmt.Errorf("lock paths must be regular files without aliases: %w", errs.ErrValidation)
		}
	}

	return nil
}

// openVerifiedLockFile opens the lock file and confirms the descriptor it got
// is the same regular, single-linked file the path still names.
//
// Opening by name and then trusting the name is the gap this closes: between
// the open and the flock, the path can be replaced, and a lock held on a
// descriptor nobody else will open serialises nothing.
func openVerifiedLockFile(root *os.Root, name, display string) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", display, err)
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, err
	}

	current, err := root.Lstat(name)
	if err != nil || !opened.Mode().IsRegular() || !singleLink(opened) ||
		!os.SameFile(opened, current) || !current.Mode().IsRegular() {
		_ = file.Close()

		return nil, fmt.Errorf("lock file changed while opening: %w", errs.ErrValidation)
	}

	return file, nil
}
