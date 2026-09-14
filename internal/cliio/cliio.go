// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cliio bridges file paths and stdin/stdout for CLI flags.
// CLI conventions (see clig.dev) say user-provided file paths should
// accept `-` to mean stdin (read) or stdout (write), so a pipe like
//
//	curl https://… | reusable-ci config validate --file -
//	reusable-ci publish npm write-npmrc --output - > ~/.npmrc
//
// works without a tempfile. This package exposes the minimal helpers
// to honour that convention.
//
// Use these for paths that come from a CLI flag or positional arg.
// Internal fixed paths (package.json, gradle.properties, etc.) should
// keep using os.ReadFile / os.WriteFile directly.
package cliio

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// StdSentinel is the conventional "-" filename meaning stdin (when reading)
// or stdout (when writing).
const StdSentinel = "-"

// maxReadSize bounds ReadFile so a runaway hand-off file (a corrupt or
// maliciously huge ledger/journal/manifest) cannot OOM the job. Every
// legitimate input on this path — ledgers, journals, keys, checksums,
// changelogs, SBOM manifests — is orders of magnitude smaller; large
// binary artifacts are streamed elsewhere, never slurped through here.
const maxReadSize = 64 << 20 // 64 MiB

// ReadFile returns the bytes at path, or — when path is exactly "-" —
// the contents of stdin read to EOF. Mirrors os.ReadFile's interface
// so call sites migrate by a single-line change. Input larger than
// maxReadSize is refused rather than slurped.
//
// When path is "-" and stdin is a TTY (or /dev/null), ReadFile fails
// fast with errs.ErrUsage instead of hanging on terminal input — the
// clig.dev "don't hang on a TTY" rule.
func ReadFile(path string) ([]byte, error) {
	if path == StdSentinel {
		return readStdin(os.Stdin)
	}

	file, err := openReadFile(path)
	if err != nil {
		return nil, classifyReadError(err)
	}
	defer func() { _ = file.Close() }()

	return readRegularFile(file, path)
}

// ReadFileInRoot applies the regular-file and size policy without reopening a
// caller's confined input by an ambient pathname. It never interprets stdin.
func ReadFileInRoot(root *os.Root, path string) ([]byte, error) {
	file, err := openRootReadFile(root, path)
	if err != nil {
		return nil, classifyReadError(err)
	}
	defer func() { _ = file.Close() }()

	return readRegularFile(file, path)
}

func readRegularFile(file fs.File, path string) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, classifyReadError(err)
	}

	if !info.Mode().IsRegular() {
		if info.IsDir() {
			return nil, fmt.Errorf("%q is a directory, not a file: %w", path, errs.ErrMissingInput)
		}

		return nil, fmt.Errorf("%q is not a regular input file: %w", path, errs.ErrMissingInput)
	}

	if info.Size() > maxReadSize {
		return nil, fmt.Errorf("%s is %d MiB, larger than the %d MiB input bound: %w", path, info.Size()>>20, maxReadSize>>20, errs.ErrMalformedInput)
	}

	body, err := readBounded(file)
	if err != nil {
		return nil, classifyReadError(err)
	}

	return body, nil
}

// classifyReadError preserves the original cause while classifying missing and
// unreadable inputs. Directory classification uses the opened descriptor above:
// re-statting a pathname could inspect a replacement or an unrelated cwd entry.
func classifyReadError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %w", err, errs.ErrMissingInput)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", err, errs.ErrPermissionDenied)
	}

	return err
}

// WriteFile writes data to path, or — when path is exactly "-" —
// writes to stdout. Mirrors os.WriteFile's interface. The perm argument
// is ignored in the stdout case.
//
// File writes are atomic (see writeFileAtomic): a crash or kill mid-write
// never leaves a truncated file, so a retried CI step always finds either
// the previous complete artifact or the new one.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	if path == StdSentinel {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("write to stdout: %w", err)
		}

		return nil
	}

	return writeFileAtomic(path, data, perm)
}

// writeFileAtomic writes data to path crash-safely: it writes a sibling
// temp file, flushes it, and renames it over path. rename(2) is atomic on
// POSIX, so a crash, kill, or ENOSPC mid-write leaves either the previous
// complete file or the new complete file — never a half-written one that
// would block a re-run (e.g. a truncated ledger that no longer parses).
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	// The temp must share the destination's directory (hence filesystem)
	// for the rename to be atomic.
	tmp, err := os.CreateTemp(dir, base+".tmp-*") //nolint:varnamelen // 'tmp' is the idiomatic name for the temp file.
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}

	committed := false

	defer func() {
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp for %s: %w", path, err)
	}

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("sync temp for %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}

	committed = true

	return nil
}

// CreateWriter opens path for writing (O_CREATE|O_WRONLY|O_TRUNC), or —
// when path is exactly "-" — returns a writer that targets stdout. The
// returned io.WriteCloser must be Closed by the caller; the stdout-bound
// closer is a no-op, so a single `defer w.Close()` is correct in both
// modes.
//
// O_TRUNC is the half that is easy to lose and silent when lost: writing
// shorter content over a longer existing file must leave the file shorter, not
// leave the old tail behind. A truncated-looking SBOM with the previous run's
// JSON after it is still valid-ish text and parses as something.
//
// The destination must not be a symlink; see openWriteNoFollow.
func CreateWriter(path string, perm fs.FileMode) (io.WriteCloser, error) {
	if path == StdSentinel {
		return stdoutWriter{}, nil
	}

	return openWriteNoFollow(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
}

// stdoutWriter is a no-op-closing writer that targets os.Stdout. Using
// a typed wrapper (rather than an inline anonymous struct) keeps the
// CreateWriter return value reflectable in tests.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdoutWriter) Close() error                { return nil }

// AppendLines appends one line per entry to path, creating it 0644 when
// absent — the shape of the runner's env and path files ($GITHUB_ENV,
// $FORGEJO_PATH, …). The runner reads those files line by line, so an entry
// containing a line break would smuggle a second entry in; such an entry is
// refused before anything is written.
func AppendLines(path string, lines ...string) error {
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n") {
			return fmt.Errorf("refusing to append to %s: an entry contains a line break: %w", path, errs.ErrValidation)
		}
	}

	file, err := openWriteNoFollow(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	if err := writeLinesAndClose(file, lines); err != nil {
		return fmt.Errorf("append to %s: %w", path, err)
	}

	return nil
}

// writeLinesAndClose writes each line and closes the destination, reporting
// both failures when both happen.
//
// Close is not a formality on an appended file: a write can succeed into the
// page cache and the flush fail at close, so discarding the close error would
// report a successful append that never reached the disk. For $GITHUB_ENV that
// means later steps silently missing a variable the run believed it had set.
//
// It takes an io.WriteCloser rather than the *os.File so a close failure can be
// injected. The delayed-close case cannot be produced portably against a real
// file, and stating the join logic in a comment was the evidence this had
// before; a seam is the difference between describing the behaviour and
// checking it.
func writeLinesAndClose(dst io.WriteCloser, lines []string) error {
	var err error

	for _, line := range lines {
		if _, err = fmt.Fprintln(dst, line); err != nil {
			break
		}
	}

	return errors.Join(err, dst.Close())
}

// OpenAppendNoFollow opens a runner-supplied output file for appending and
// refuses a symlinked destination.
//
// It exists because three places write files whose PATH arrives in the
// environment: this package's AppendLines ($GITHUB_ENV, $FORGEJO_PATH), the
// GitHub Actions output sink ($GITHUB_OUTPUT) and the GitLab dotenv sink. All
// three opened the path directly and followed whatever link was there, and
// each had its own copy of the open. One hardened entry point is the fix; a
// fourth copy would have been the next bug.
func OpenAppendNoFollow(path string, perm fs.FileMode) (*os.File, error) {
	return openWriteNoFollow(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
}

// openWriteNoFollow opens a destination for writing and refuses when the final
// path component is a symlink.
//
// The three output paths in this package used to disagree about this, and two
// of them wrote somewhere the caller never named. WriteFile renames a temporary
// file into place, which REPLACES a symlink and leaves its target untouched.
// CreateWriter and AppendLines opened the path directly, which FOLLOWS the link
// and writes through to whatever it points at.
//
// WriteFile keeps its behaviour, because replacing the link already satisfies
// the property: the write lands at the path the caller named and the link's
// target is never touched. The two that follow the link cannot get there by
// replacing — for them the equivalent would be silently discarding a link the
// caller may not have known was there — so they refuse instead. The shared rule
// is that no output path in this package writes THROUGH a link to another file.
//
// That difference matters because these destinations are not always the
// program's own choice. AppendLines writes the runner's $GITHUB_ENV and
// $FORGEJO_PATH files, whose paths arrive in the environment; CreateWriter
// writes report and SBOM destinations that arrive as flag values. Anything able
// to place a symlink at one of those paths could redirect the write.
//
// Refusing is the narrow fix, and narrow matters: O_NOFOLLOW rejects only when
// the FINAL component is a link, so a runner handing over a real file — which
// is what runners do — is unaffected, and only the substitution case fails.
// A caller that genuinely means "write through this link" can resolve it first
// and say so.
func openWriteNoFollow(path string, flag int, perm fs.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm) //nolint:gosec // path is a CLI-flag or runner-supplied value; perm is caller-supplied.
	if err == nil {
		return file, nil
	}

	// ELOOP is what O_NOFOLLOW reports for a symlinked final component. Saying
	// which path was refused and why is the whole value of refusing here.
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("refusing to write through the symlink at %s: %w", path, errs.ErrValidation)
	}

	return nil, err
}
