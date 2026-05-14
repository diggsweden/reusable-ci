// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package ghaoutput implements ci.OutputSink for the GitHub Actions
// $GITHUB_OUTPUT file. Multiline values use the standard heredoc protocol
// with a randomised delimiter.
//
// When neither GITHUB_OUTPUT nor CI_OUTPUT is set (e.g. local dev runs
// without the test harness), the sink writes to /dev/null — Set/SetMultiline
// succeed silently.
package ghaoutput

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// Sink writes to the configured output file path, opening on first use.
type Sink struct {
	path   string
	mu     sync.Mutex
	w      io.WriteCloser
	closed bool
}

// NewFromEnv returns a Sink targeting $GITHUB_OUTPUT (preferred) or
// $CI_OUTPUT. If neither is set, returns a /dev/null sink so callers
// can always Set/SetMultiline without checking.
func NewFromEnv() *Sink {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		path = os.Getenv("CI_OUTPUT")
	}
	if path == "" {
		path = os.DevNull
	}
	return &Sink{path: path}
}

// New returns a Sink writing to the given path. Useful for tests that
// don't want to touch process env.
func New(path string) *Sink { return &Sink{path: path} }

func (s *Sink) ensureOpen() error {
	if s.w != nil {
		return nil
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %q: %w", s.path, err)
	}
	s.w = f
	return nil
}

// Set writes "key=value\n" to the output file.
func (s *Sink) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("ghaoutput: Set after Close")
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("ghaoutput: SetMultiline after Close")
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

func randomDelimiter() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "EOF_" + hex.EncodeToString(buf[:]), nil
}

// Compile-time check.
var _ ci.OutputSink = (*Sink)(nil)
