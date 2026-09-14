//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpgkey

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// The lock file lives in the shared temp directory, which is the one place
// every test binary in a run can find — and also a place anyone on the machine
// can create a name first. O_CREATE follows symlinks, so without O_NOFOLLOW a
// planted link would redirect the open and the flock somewhere else and the
// tests would proceed believing they held a lock.
func TestOpenLockNoFollow_RefusesAPlantedSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	link := filepath.Join(dir, "lock")

	require.NoError(t, os.WriteFile(target, []byte("not the lock\n"), 0o600))
	require.NoError(t, os.Symlink(target, link))

	file, err := openLockNoFollow(link)
	if err == nil {
		_ = file.Close()
		t.Fatal("a symlinked lock path was opened; the flock would have been taken on another file")
	}

	body, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, "not the lock\n", string(body), "the link's target was modified")
}

// A path that is not a regular file must be refused. Two different mechanisms
// do it, and a fault replay showed the difference matters: a DIRECTORY is
// rejected by the kernel, because O_RDWR on one is EISDIR, so that case never
// reaches the explicit type check. A FIFO is not — O_RDWR opens it happily —
// and flocking a FIFO would look like holding a lock while serialising nothing.
// The type check is what covers the second, so it needs the second to prove it.
func TestOpenLockNoFollow_RefusesANonRegularPath(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create func(t *testing.T, path string)
		why    string
	}{
		{
			name:   "a directory",
			create: func(t *testing.T, path string) { require.NoError(t, os.Mkdir(path, 0o700)) },
			why:    "refused by the kernel: O_RDWR on a directory is EISDIR",
		},
		{
			name:   "a FIFO",
			create: func(t *testing.T, path string) { require.NoError(t, syscall.Mkfifo(path, 0o600)) },
			why:    "opens fine, so only the explicit type check refuses it; flocking it would serialise nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock")
			tc.create(t, path)

			file, err := openLockNoFollow(path)
			if err == nil {
				_ = file.Close()
				t.Fatalf("%s was accepted as the lock file: %s", tc.name, tc.why)
			}
		})
	}
}

// The control: an ordinary path still opens, and reopening the same path is
// what makes it a shared lock rather than a fresh file each time.
func TestOpenLockNoFollow_OpensAndReopensARegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")

	first, err := openLockNoFollow(path)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := openLockNoFollow(path)
	require.NoError(t, err)

	defer func() { _ = second.Close() }()

	info, err := second.Stat()
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
}

// The lock file is deliberately not removed. Unlinking it while another process
// holds the flock would let the next process create a fresh file at the same
// path and take a second, independent lock — losing the mutual exclusion the
// file exists to provide. Pinning it here so the intent survives the next
// reader who notices the leftover file.
func TestGlobalLockPath_IsUserScopedAndKept(t *testing.T) {
	require.Contains(t, globalLockPath, strconv.Itoa(os.Getuid()),
		"the lock path is shared across users; two people on one machine would block each other")
	require.Equal(t, os.TempDir(), filepath.Dir(globalLockPath),
		"the lock must be somewhere every test binary in the run agrees on")
}
