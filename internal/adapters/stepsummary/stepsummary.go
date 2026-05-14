// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package stepsummary implements ci.SummarySink as an append-only writer
// for $GITHUB_STEP_SUMMARY (GitHub Actions) or a configured fallback
// path (CI_SUMMARY_FILE — used by the bash on GitLab).
//
// When neither env is set the adapter is a no-op (Append returns nil
// without writing anywhere). That matches the bash, which silently
// drops `$(ci_summary_file)` redirections when the env is unset.
package stepsummary

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// Sink writes append-only markdown to a step-summary file.
type Sink struct {
	path string
	mu   sync.Mutex
}

// NewFromEnv reads $GITHUB_STEP_SUMMARY first, then $CI_SUMMARY_FILE.
// Empty string in both produces a no-op Sink.
func NewFromEnv() *Sink {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		path = os.Getenv("CI_SUMMARY_FILE")
	}
	return &Sink{path: path}
}

// New returns a Sink writing to the given path. Empty path → no-op.
func New(path string) *Sink { return &Sink{path: path} }

// Append implements ci.SummarySink.
func (s *Sink) Append(_ context.Context, markdown string) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open step summary %q: %w", s.path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(markdown); err != nil {
		return fmt.Errorf("write step summary: %w", err)
	}
	return nil
}

// Compile-time conformance check.
var _ ci.SummarySink = (*Sink)(nil)
