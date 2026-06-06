// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package ghaoutput implements ci.OutputSink for the GitHub Actions
// $GITHUB_OUTPUT file. Multiline values use the standard heredoc protocol
// with a randomised delimiter.
//
// When GITHUB_OUTPUT is not set (e.g. local dev runs without the test harness),
// the sink writes to /dev/null so Set/SetMultiline succeed silently.
package ghaoutput

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// errSinkClosed is returned by Set/SetMultiline after Close. It lets
// callers branch with errors.Is without string-matching the message.
var errSinkClosed = errors.New("output sink is closed")

// Sink writes to the configured output file path, opening on first use.
type Sink struct {
	path   string
	mu     sync.Mutex
	w      io.WriteCloser
	closed bool
}

// NewFromEnv returns a Sink targeting $GITHUB_OUTPUT. If it is unset, returns a
// /dev/null sink so callers can always Set/SetMultiline without checking.
func NewFromEnv() *Sink {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		path = os.DevNull
	}

	return &Sink{path: path}
}

// New returns a Sink writing to the given path. Useful for tests that
// don't want to touch process env.
func New(path string) *Sink { return &Sink{path: path} }

// SetBool writes the bool value formatted as "true" / "false" per GHA
// convention so downstream `if: steps.X.outputs.Y == 'true'` works.
func (s *Sink) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

// Set writes "key=value\n" to the output file.
func (s *Sink) Set(_ context.Context, key, value string) error {
	if !validOutputKey(key) {
		return fmt.Errorf("ghaoutput: invalid output key %q: %w", key, errs.ErrValidation)
	}

	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("ghaoutput: scalar output %q contains a newline; use SetMultiline: %w", key, errs.ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("ghaoutput: Set: %w", errSinkClosed)
	}

	if err := s.ensureOpen(); err != nil {
		return err
	}

	_, err := fmt.Fprintf(s.w, "%s=%s\n", key, value)

	return err
}

// SetMultiline writes the heredoc-encoded form expected by GitHub Actions:
//
//	<key><<<delim>
//	<line>
//	...
//	<delim>
//
// The delimiter is a random hex string so it never collides with content.
func (s *Sink) SetMultiline(_ context.Context, key string, lines []string) error {
	if !validOutputKey(key) {
		return fmt.Errorf("ghaoutput: invalid output key %q: %w", key, errs.ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("ghaoutput: SetMultiline: %w", errSinkClosed)
	}

	if err := s.ensureOpen(); err != nil {
		return err
	}

	delim, err := randomDelimiter()
	if err != nil {
		return fmt.Errorf("delimiter: %w", err)
	}

	if _, err := fmt.Fprintf(s.w, "%s<<%s\n", key, delim); err != nil {
		return err
	}

	for _, line := range lines {
		if _, err := fmt.Fprintln(s.w, line); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(s.w, delim); err != nil {
		return err
	}

	return nil
}

// Close flushes the underlying file. After Close, Set and SetMultiline
// return an error.
func (s *Sink) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.w != nil {
		err := s.w.Close()
		s.w = nil

		return err
	}

	return nil
}

func (s *Sink) ensureOpen() error {
	if s.w != nil {
		return nil
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec,varnamelen // $GITHUB_OUTPUT file read by the runner; 'f' is idiomatic for the os.File handle.
	if err != nil {
		// os.OpenFile already returns "open <path>: <syscall err>" via
		// *fs.PathError. Adding our own "open <path>:" prefix would
		// duplicate both the verb and the path; pull the underlying
		// syscall error out so the chain reads cleanly.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return fmt.Errorf("$GITHUB_OUTPUT path %q: %w: %w", s.path, pathErr.Err, errs.ErrInvalidConfig)
		}

		return fmt.Errorf("$GITHUB_OUTPUT path %q: %w: %w", s.path, err, errs.ErrInvalidConfig)
	}

	s.w = f

	return nil
}

func randomDelimiter() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}

	return "EOF_" + hex.EncodeToString(buf[:]), nil
}

func validOutputKey(key string) bool {
	if key == "" {
		return false
	}

	for i, r := range key {
		if !validKeyRune(r, i) {
			return false
		}
	}

	return true
}

func validKeyRune(r rune, position int) bool {
	switch {
	case isASCIIAlpha(r), r == '_':
		return true
	case position > 0 && (isASCIIDigit(r) || r == '-'):
		return true
	}

	return false
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// Compile-time check.
var _ ci.OutputSink = (*Sink)(nil)
