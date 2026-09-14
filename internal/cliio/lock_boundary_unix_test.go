// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build unix

package cliio

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLockBoundary_AcquireFailure(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "lock"))
	require.NoError(t, err)
	require.NoError(t, file.Close())
	// File.Fd returns an invalid descriptor after Close, not a reusable raw FD.
	require.ErrorIs(t, lockExclusive(file), syscall.EBADF)
}

func TestLockBoundary_UnlockFailure(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "lock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	require.NoError(t, lockExclusive(file))
	require.NoError(t, file.Close())
	require.ErrorIs(t, unlock(file), syscall.EBADF)
}

func TestLockBoundary_UnlockBeforeClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := os.Create(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	second, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, lockExclusive(first))
	require.ErrorIs(t, syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB), syscall.EWOULDBLOCK)
	require.NoError(t, unlock(first))
	// Keep first open: closing it would mask a missing explicit unlock.
	require.NoError(t, syscall.Flock(int(second.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	require.NoError(t, unlock(second))
}

func TestLockBoundary_CallbackError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	failure := errors.New("callback failure") //nolint:err113 // unique sentinel proves unchanged callback error identity.
	calls := 0
	err := WithLock(path, func() error {
		calls++

		return failure
	})
	require.Same(t, failure, err)
	require.Equal(t, 1, calls)

	file, err := os.OpenFile(path+".lock", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	require.NoError(t, syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	require.NoError(t, unlock(file))
}
