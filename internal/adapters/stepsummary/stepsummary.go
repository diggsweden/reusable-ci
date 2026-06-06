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
	"os"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// Sink writes append-only markdown to a step-summary file.
type Sink struct {
	path string
	mu   sync.Mutex
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

// Compile-time conformance check.
var _ ci.SummarySink = (*Sink)(nil)
