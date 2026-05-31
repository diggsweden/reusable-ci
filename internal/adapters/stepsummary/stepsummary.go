// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package stepsummary implements ci.SummarySink as an append-only writer for a
// configured per-step markdown summary path.
//
// When the path is empty the adapter is a no-op (Append returns nil without
// writing anywhere).
package stepsummary

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
)

// Sink writes append-only markdown to a step-summary file. When no file
// is configured it either no-ops or, if a log fallback is set, echoes the
// markdown to that writer — used for runners (Forgejo/Gitea) that don't
// expose a summary file, so the content lands in the job log instead of
// being silently dropped (go-gitea/gitea#27898, nektos/act#1187).
type Sink struct {
	path string
	log  io.Writer // fallback when path == "" (nil → silent no-op)
	mu   sync.Mutex
}

// New returns a Sink writing to the given path. Empty path → no-op.
func New(path string) *Sink { return &Sink{path: path} }

// NewWithLog returns a Sink that writes to path when set, and otherwise
// echoes the markdown to log (the job log) rather than dropping it. Used
// for the Forgejo runner, whose summary file may be absent.
func NewWithLog(path string, log io.Writer) *Sink { return &Sink{path: path, log: log} }

// Append implements ci.SummarySink.
func (s *Sink) Append(_ context.Context, markdown string) error {
	if s == nil {
		return nil
	}

	if s.path == "" {
		return s.appendToLog(markdown)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec,varnamelen // step-summary file read by GitHub Actions runner.
	if err != nil {
		return fmt.Errorf("open step summary %q: %w", s.path, err)
	}

	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(markdown); err != nil {
		return fmt.Errorf("write step summary: %w", err)
	}

	return nil
}

// appendToLog echoes the summary markdown to the fallback writer (no-op
// when unset). Serialised so concurrent appends don't interleave.
func (s *Sink) appendToLog(markdown string) error {
	if s.log == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := io.WriteString(s.log, markdown); err != nil {
		return fmt.Errorf("write step summary to log: %w", err)
	}

	return nil
}

// Compile-time conformance check.
var _ ci.SummarySink = (*Sink)(nil)
