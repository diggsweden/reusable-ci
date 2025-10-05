// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

func TestReadBoundary_GrowthDuringRead(t *testing.T) {
	for _, kind := range []string{"unchanged path", "directory replacement"} {
		t.Run(kind, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "growing"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = file.Close() })
			require.NoError(t, file.Truncate(1024))

			reads := 0
			input := readBoundaryFile{File: file, read: func(data []byte) (int, error) {
				reads++
				if reads == 2 {
					// Grow synchronously after reading starts, with no writer race or
					// physical 64 MiB fixture. Stat on this same descriptor passed first.
					require.NoError(t, file.Truncate(maxReadSize+4096))

					if kind == "directory replacement" {
						require.NoError(t, os.Rename(file.Name(), file.Name()+".opened"))
						require.NoError(t, os.Mkdir(file.Name(), 0o700))
					}
				}

				return file.Read(data)
			}}
			body, err := readRegularFile(input, file.Name())
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.NotErrorIs(t, err, errs.ErrMissingInput)
			require.ErrorContains(t, err, "input exceeds")
			require.Nil(t, body)
			require.GreaterOrEqual(t, reads, 2)

			position, err := file.Seek(0, io.SeekCurrent)
			require.NoError(t, err)
			require.EqualValues(t, maxReadSize+1, position)
		})
	}
}

func TestReadBoundary_RootedErrorsIgnoreCWD(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, root.WriteFile("input", []byte("valid rooted input"), 0o600))
	require.NoError(t, root.Mkdir("directory", 0o700))
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("input", 0o700))
	require.NoError(t, os.WriteFile("directory", []byte("unrelated cwd file"), 0o600))

	body, err := ReadFileInRoot(root, "input")
	require.NoError(t, err)
	require.Equal(t, "valid rooted input", string(body))
	body, err = ReadFileInRoot(root, "directory")
	require.ErrorIs(t, err, errs.ErrMissingInput)
	require.ErrorContains(t, err, "is a directory")
	require.Nil(t, body)

	file, err := root.Open("input")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	failure := errors.New("rooted descriptor read failure") //nolint:err113 // unique sentinel distinguishes the read failure from path policy.
	for _, test := range []struct {
		name  string
		cause error
		class error
	}{
		{name: "read", cause: failure, class: failure},
		{name: "permission", cause: fs.ErrPermission, class: errs.ErrPermissionDenied},
		{name: "missing", cause: fs.ErrNotExist, class: errs.ErrMissingInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			input := readBoundaryFile{File: file, read: func(data []byte) (int, error) {
				calls++

				return copy(data, "partial"), test.cause
			}}
			got, readErr := readRegularFile(input, "input")
			require.ErrorIs(t, readErr, test.cause)
			require.ErrorIs(t, readErr, test.class)

			if !errors.Is(test.class, errs.ErrMissingInput) {
				require.NotErrorIs(t, readErr, errs.ErrMissingInput)
			}

			require.Nil(t, got)
			require.Equal(t, 1, calls)
		})
	}

	t.Run("descriptor stat", func(t *testing.T) {
		require.NoError(t, file.Close())
		got, statErr := readRegularFile(file, "input")
		require.ErrorIs(t, statErr, os.ErrClosed)
		require.NotErrorIs(t, statErr, errs.ErrMissingInput)
		require.Nil(t, got)
	})
	t.Run("root open", func(t *testing.T) {
		require.NoError(t, root.Close())
		got, openErr := ReadFileInRoot(root, "input")
		require.ErrorIs(t, openErr, os.ErrClosed)
		require.NotErrorIs(t, openErr, errs.ErrMissingInput)
		require.Nil(t, got)
	})
}

func TestReadBoundary_PartialReadError(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "input"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	failure := errors.New("read failure after bytes") //nolint:err113 // unique sentinel distinguishes this read failure from type/size guards.

	for _, source := range []string{"file", "stdin"} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			input := readBoundaryFile{File: file, read: func(data []byte) (int, error) {
				calls++

				return copy(data, "partial"), failure
			}}

			var (
				body    []byte
				readErr error
			)
			if source == "stdin" {
				body, readErr = readStdin(input)
				require.ErrorContains(t, readErr, "read from stdin")
			} else {
				body, readErr = readRegularFile(input, file.Name())
			}

			require.ErrorIs(t, readErr, failure)
			require.Nil(t, body)
			require.Equal(t, 1, calls)
		})
	}
}

func TestReadBoundary_StdinSizes(t *testing.T) {
	for _, test := range []struct {
		name string
		size int64
	}{
		{name: "empty", size: 0},
		{name: "at cap", size: maxReadSize},
		{name: "over cap", size: maxReadSize + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = file.Close() })
			require.NoError(t, file.Truncate(test.size))

			if test.size != 0 {
				_, err = file.WriteAt([]byte("Z"), test.size-1)
				require.NoError(t, err)
			}

			original := os.Stdin
			os.Stdin = file

			t.Cleanup(func() { os.Stdin = original })
			require.False(t, StdinIsCharDevice())

			body, err := ReadFile(StdSentinel)
			if test.size > maxReadSize {
				require.ErrorIs(t, err, errs.ErrMalformedInput)
				require.Nil(t, body)
			} else {
				require.NoError(t, err)
				require.NotNil(t, body)
				require.Len(t, body, int(test.size))

				if test.size != 0 {
					require.Equal(t, byte('Z'), body[len(body)-1])
				}
			}
		})
	}
}

func TestReadBoundary_StdinErrors(t *testing.T) {
	for _, kind := range []string{"nil", "closed", "directory", "write only"} {
		t.Run(kind, func(t *testing.T) {
			var (
				file *os.File
				err  error
			)

			switch kind {
			case "nil":
			case "directory":
				file, err = os.Open(t.TempDir())
			case "closed", "write only":
				file, err = os.OpenFile(filepath.Join(t.TempDir(), "stdin"), os.O_CREATE|os.O_WRONLY, 0o600)
			}

			require.NoError(t, err)

			if file != nil {
				t.Cleanup(func() { _ = file.Close() })

				if kind == "closed" {
					require.NoError(t, file.Close())
				}
			}

			original := os.Stdin
			os.Stdin = file

			t.Cleanup(func() { os.Stdin = original })
			require.False(t, StdinIsCharDevice())

			body, err := ReadFile(StdSentinel)
			require.Error(t, err)
			require.Nil(t, body)

			switch kind {
			case "nil":
				require.ErrorIs(t, err, os.ErrInvalid)
			case "closed":
				require.ErrorIs(t, err, os.ErrClosed)
			case "directory", "write only":
				require.ErrorContains(t, err, "read from stdin")
			}
		})
	}
}

type readBoundaryFile struct {
	fs.File
	read func([]byte) (int, error)
}

func (file readBoundaryFile) Read(data []byte) (int, error) { return file.read(data) }
