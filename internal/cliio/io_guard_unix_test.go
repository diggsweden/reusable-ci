// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build unix

package cliio

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestLockGuard_RejectsAliasesWithoutCallbacks(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"symlink", "parent", "hardlink", "lock symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			file := filepath.Join(root, "data")
			require.NoError(t, os.WriteFile(file, []byte("keep"), 0o600))
			path := file

			switch kind {
			case "symlink":
				path = filepath.Join(root, "alias")
				require.NoError(t, os.Symlink(file, path))
			case "parent":
				alias := filepath.Join(t.TempDir(), "parent")
				require.NoError(t, os.Symlink(root, alias))
				path = filepath.Join(alias, "data")
			case "hardlink":
				path = filepath.Join(root, "alias")
				require.NoError(t, os.Link(file, path))
			case "lock symlink":
				require.NoError(t, os.Symlink(file, file+".lock"))
			}

			called := false
			err := WithLock(path, func() error {
				called = true

				return nil
			})
			require.ErrorIs(t, err, errs.ErrValidation)
			require.False(t, called)

			body, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "keep", string(body))
		})
	}
}

func TestLockGuardChild(t *testing.T) {
	if os.Getenv("RC_LOCK_CHILD") != "1" {
		return
	}

	_, err := fmt.Fprintln(os.Stdout, "ready")
	require.NoError(t, err)
	require.NoError(t, WithLock(os.Getenv("RC_LOCK_PATH"), func() error { return os.WriteFile(os.Getenv("RC_LOCK_ENTERED"), []byte("entered"), 0o600) })) //nolint:gosec // closed child environment supplies only parent-owned fixture paths.
}

func TestLockGuard_SerializesOwnedChild(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data")
	entered := filepath.Join(root, "entered")
	binary, err := os.Executable()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	child := exec.CommandContext(ctx, binary, "-test.run=^TestLockGuardChild$") //nolint:gosec // current test binary only, fixed helper selection.
	child.Env = []string{"HOME=" + root, "TMPDIR=" + root, "RC_LOCK_CHILD=1", "RC_LOCK_PATH=" + path, "RC_LOCK_ENTERED=" + entered}
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, WithLock(path, func() error {
		require.NoError(t, child.Start())

		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "ready\n", line)
		time.Sleep(50 * time.Millisecond)

		_, statErr := os.Stat(entered)
		require.ErrorIs(t, statErr, os.ErrNotExist)

		return nil
	}))
	require.NoError(t, child.Wait())

	body, err := os.ReadFile(entered)
	require.NoError(t, err)
	require.Equal(t, "entered", string(body))
}

func TestReadFileGuard_BoundsFilesAliasesAndRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input")
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(maxReadSize))
	require.NoError(t, file.Close())

	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(path, alias))

	for _, name := range []string{path, alias} {
		body, readErr := ReadFile(name)
		require.NoError(t, readErr)
		require.Len(t, body, maxReadSize)
	}

	require.NoError(t, os.Truncate(path, maxReadSize+1))

	for _, name := range []string{path, alias} {
		body, readErr := ReadFile(name)
		require.ErrorIs(t, readErr, errs.ErrMalformedInput)
		require.Nil(t, body)
	}

	fifo := filepath.Join(root, "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	peer, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0) //nolint:gosec // owned FIFO; supplies EOF even when replaying the old reader.
	require.NoError(t, err)

	defer func() { _ = peer.Close() }()

	_, err = peer.WriteString("fixture")
	require.NoError(t, err)

	closePeer := time.AfterFunc(10*time.Millisecond, func() { _ = peer.Close() })
	defer closePeer.Stop()

	_, err = ReadFile(fifo)
	require.ErrorIs(t, err, errs.ErrMissingInput)
}
