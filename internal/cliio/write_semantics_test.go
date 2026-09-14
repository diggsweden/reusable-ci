// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cliio_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

// This package has three ways to produce an output file and they had three
// different answers to the same question: what happens when the destination is
// a symlink?
//
//	WriteFile    renamed a temp file into place, REPLACING the link.
//	CreateWriter opened the path, writing THROUGH the link to its target.
//	AppendLines  opened the path, appending THROUGH the link to its target.
//
// Two of the three wrote to a file the caller never named. That is not
// theoretical here: AppendLines writes the runner's $GITHUB_ENV and
// $FORGEJO_PATH, whose paths arrive in the environment, and CreateWriter writes
// report and SBOM destinations that arrive as flag values.
//
// The property they agree on now is the one that matters: NO output path writes
// through a symlink to another file. The mechanism differs, and that is fine
// because it is written down. WriteFile replaces the link with a real file at
// the path the caller named — the write lands where it was aimed. CreateWriter
// and AppendLines refuse, because for them "write where it was aimed" would
// mean silently discarding the link the caller may not have known was there.
//
// The refusal is narrow on purpose: only the final component being a link
// fails, so a runner handing over a real file behaves exactly as before.

func TestOutputPaths_RefuseASymlinkedDestination(t *testing.T) {
	t.Parallel()

	const original = "ORIGINAL TARGET\n"

	for _, tc := range []struct {
		name    string
		write   func(path string) error
		refuses bool
		why     string
	}{
		{
			name:  "WriteFile",
			write: func(path string) error { return cliio.WriteFile(path, []byte("NEW\n"), 0o600) },
			why: "replaces the link with a real file at the named path, so the write lands where it was " +
				"aimed and the target is never touched",
		},
		{
			name:    "CreateWriter",
			refuses: true,
			why:     "used to truncate and overwrite the link's TARGET",
			write: func(path string) error {
				w, err := cliio.CreateWriter(path, 0o600)
				if err != nil {
					return err
				}

				if _, err := w.Write([]byte("NEW\n")); err != nil {
					return errors.Join(err, w.Close())
				}

				return w.Close()
			},
		},
		{
			name:    "AppendLines",
			refuses: true,
			write:   func(path string) error { return cliio.AppendLines(path, "NEW") },
			why:     "used to append to the link's TARGET; this one writes the runner's env file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "target.txt")
			link := filepath.Join(dir, "link.txt")

			require.NoError(t, os.WriteFile(target, []byte(original), 0o600))
			require.NoError(t, os.Symlink(target, link))

			err := tc.write(link)

			if tc.refuses {
				require.Errorf(t, err, "writing through a symlink was accepted: %s", tc.why)
				require.ErrorIs(t, err, errs.ErrValidation, "the refusal must be classified, not an opaque OS error")
			} else {
				require.NoErrorf(t, err, "%s is documented as replacing the link, not refusing it: %s", tc.name, tc.why)

				info, statErr := os.Lstat(link)
				require.NoError(t, statErr)
				require.Zerof(t, info.Mode()&os.ModeSymlink,
					"%s left a symlink in place, so the next write would follow it", tc.name)
			}

			// The shared property, whichever mechanism got there.
			body, readErr := os.ReadFile(target) //nolint:gosec // owned temporary file.
			require.NoError(t, readErr)
			require.Equalf(t, original, string(body),
				"the symlink's target was modified by a write aimed at the link: %s", tc.why)
		})
	}
}

// The control: a real file at the same path must still work, or the refusal
// above is indistinguishable from "these functions stopped writing".
func TestOutputPaths_StillWriteToARealFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write := filepath.Join(dir, "write.txt")
	require.NoError(t, cliio.WriteFile(write, []byte("written\n"), 0o600))
	require.Equal(t, "written\n", readBack(t, write))

	stream := filepath.Join(dir, "stream.txt")
	writer, err := cliio.CreateWriter(stream, 0o600)
	require.NoError(t, err)
	_, err = writer.Write([]byte("streamed\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Equal(t, "streamed\n", readBack(t, stream))

	appended := filepath.Join(dir, "append.txt")
	require.NoError(t, cliio.AppendLines(appended, "one", "two"))
	require.NoError(t, cliio.AppendLines(appended, "three"))
	require.Equal(t, "one\ntwo\nthree\n", readBack(t, appended))
}

// A shorter write over a longer file must leave the file shorter.
//
// Without O_TRUNC the old tail survives, and the result is the worst kind of
// corruption: a report or SBOM that still parses, because the new document is
// followed by the remains of the previous run's. Nothing errors and nothing
// looks wrong until something downstream reads the second half.
func TestCreateWriter_TruncatesRatherThanOverwritingInPlace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	const seeded = `{"previous":"run","with":"a much longer body than the replacement"}` + "\n"

	require.NoError(t, os.WriteFile(path, []byte(seeded), 0o600))

	writer, err := cliio.CreateWriter(path, 0o600)
	require.NoError(t, err)

	const replacement = `{"n":1}` + "\n"

	_, err = writer.Write([]byte(replacement))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	require.Equal(t, replacement, readBack(t, path),
		"the previous contents survived past the new ones; a consumer would read both")
}

// WriteFile replaces atomically, so the same property holds for it, by a
// different mechanism worth asserting separately.
func TestWriteFile_ShorterContentReplacesLonger(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "out.txt")
	require.NoError(t, os.WriteFile(path, []byte("a long seeded body that must not survive\n"), 0o600))
	require.NoError(t, cliio.WriteFile(path, []byte("short\n"), 0o600))
	require.Equal(t, "short\n", readBack(t, path))
}

func readBack(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // owned temporary file.
	require.NoError(t, err)

	return string(body)
}

// failingCloser reports a write and/or close failure on demand, which is the
// only way to reach the delayed-close path: a real file's flush cannot be made
// to fail portably.
type failingCloser struct {
	written  []string
	writeErr error
	closeErr error
	closed   int
}

func (f *failingCloser) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	f.written = append(f.written, string(p))

	return len(p), nil
}

func (f *failingCloser) Close() error {
	f.closed++

	return f.closeErr
}

// A close failure on an appended file is not a formality: the write can land in
// the page cache and the flush fail at close. Reporting success then would mean
// a later step silently missing a $GITHUB_ENV variable the run believed it set.
func TestWriteLinesAndClose_ReportsBothFailures(t *testing.T) {
	t.Parallel()

	writeFailure := errors.New("write failed") //nolint:err113 // injected identity is the contract.
	closeFailure := errors.New("close failed") //nolint:err113 // injected identity is the contract.

	for _, tc := range []struct {
		name      string
		dst       *failingCloser
		wantWrite bool
		wantClose bool
		wantLines int
		why       string
	}{
		{
			name: "both succeed", dst: &failingCloser{}, wantLines: 2,
			why: "the ordinary path; without it the assertions below could pass on a function that never writes",
		},
		{
			name: "close fails after successful writes", dst: &failingCloser{closeErr: closeFailure},
			wantClose: true, wantLines: 2,
			why: "the delayed-flush case; the lines were written and still did not reach the disk",
		},
		{
			name: "write fails", dst: &failingCloser{writeErr: writeFailure}, wantWrite: true,
			why: "and the file must still be closed, or the descriptor leaks",
		},
		{
			name: "both fail", dst: &failingCloser{writeErr: writeFailure, closeErr: closeFailure},
			wantWrite: true, wantClose: true,
			why: "neither cause may be dropped in favour of the other",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := cliio.WriteLinesAndCloseForTest(tc.dst, []string{"A=1", "B=2"})

			require.Equal(t, 1, tc.dst.closed, "the destination must be closed exactly once: %s", tc.why)
			require.Len(t, tc.dst.written, tc.wantLines)

			if !tc.wantWrite && !tc.wantClose {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)

			if tc.wantWrite {
				require.ErrorIsf(t, err, writeFailure, "the write cause was dropped: %s", tc.why)
			}

			if tc.wantClose {
				require.ErrorIsf(t, err, closeFailure, "the close cause was dropped: %s", tc.why)
			}
		})
	}
}
