// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gitlaboutput implements ci.OutputSink for GitLab CI dotenv reports.
package gitlaboutput

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// errSinkClosed is returned by Set after Close. Lets callers branch with
// errors.Is rather than string-matching the message.
var errSinkClosed = errors.New("output sink is closed")

// Sink writes scalar outputs to a dotenv file declared by the GitLab job as an
// artifacts:reports:dotenv report.
type Sink struct {
	path   string
	mu     sync.Mutex
	w      io.WriteCloser
	closed bool
}

// NewFromEnv returns a Sink targeting $CI_OUTPUT. GitLab has no built-in
// per-step output file; jobs must set CI_OUTPUT to their dotenv report path.
func NewFromEnv() *Sink { return &Sink{path: os.Getenv("CI_OUTPUT")} }

// New returns a Sink writing to path. An empty path errors on first Set so a
// GitLab output-writing command cannot silently discard values.
func New(path string) *Sink { return &Sink{path: path} }

// SetBool writes the bool value formatted as "true" / "false" so
// downstream shell-style comparisons read uniformly across providers.
func (s *Sink) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

// Set writes KEY=value to the dotenv file. Reusable-ci output keys are
// lower-hyphenated; GitLab dotenv variable names are uppercase snake case.
func (s *Sink) Set(_ context.Context, key, value string) error {
	envKey, err := dotenvKey(key)
	if err != nil {
		return err
	}

	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("gitlaboutput: scalar output %q contains a newline; use ManifestSink: %w", key, errs.ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("gitlaboutput: Set: %w", errSinkClosed)
	}

	if openErr := s.ensureOpen(); openErr != nil {
		return openErr
	}

	_, err = fmt.Fprintf(s.w, "%s=%s\n", envKey, value)

	return err
}

// SetMultiline returns an explicit error because GitLab dotenv reports only
// support single-line scalar values.
func (s *Sink) SetMultiline(_ context.Context, key string, _ []string) error {
	if _, err := dotenvKey(key); err != nil {
		return err
	}

	return fmt.Errorf("gitlaboutput: multiline output %q is unsupported; use ManifestSink: %w", key, errs.ErrUnsupported)
}

// Close releases the underlying file handle. After Close, Set returns an error.
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

	if s.path == "" {
		return fmt.Errorf("gitlaboutput: CI_OUTPUT is required for GitLab dotenv outputs: %w", errs.ErrUsage)
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // dotenv output is read by GitLab runner; 0644 expected.
	if err != nil {
		return fmt.Errorf("open %q: %w", s.path, err)
	}

	s.w = f

	return nil
}

//nolint:cyclop // dotenv key validation: one branch per allowed/disallowed rune class.
func dotenvKey(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("gitlaboutput: output key is empty: %w", errs.ErrValidation)
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for i, r := range key { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		case r == '-' && i > 0:
			b.WriteByte('_')
		case r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		default:
			return "", fmt.Errorf("gitlaboutput: invalid output key %q: %w", key, errs.ErrValidation)
		}
	}

	return b.String(), nil
}

// Compile-time check.
var _ ci.OutputSink = (*Sink)(nil)
