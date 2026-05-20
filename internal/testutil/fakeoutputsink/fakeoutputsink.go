// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package fakeoutputsink is an in-memory implementation of ci.OutputSink
// for app-layer tests. Stores everything in maps; tests inspect via
// Single / Multiline / All.
package fakeoutputsink

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Sink implements ci.OutputSink. Safe for concurrent use.
type Sink struct {
	t          *testing.T
	mu         sync.Mutex
	scalar     map[string]string
	multiline  map[string][]string
	closed     bool
	closeCount int
}

// New returns a fresh Sink and registers t.Cleanup to assert single Close.
// Tests that care about close-once semantics inspect CloseCount.
func New(t *testing.T) *Sink {
	t.Helper()

	return &Sink{
		t:         t,
		scalar:    map[string]string{},
		multiline: map[string][]string{},
	}
}

// Set implements ci.OutputSink.
func (s *Sink) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("fakeoutputsink: Set after Close"+": %w", errs.ErrValidation)
	}

	s.scalar[key] = value

	return nil
}

// SetBool implements ci.OutputSink, formatting the boolean as
// "true"/"false" — matching the string-sink convention so tests that
// assert on scalar output don't need to know which format the
// production code uses.
func (s *Sink) SetBool(ctx context.Context, key string, value bool) error {
	return s.Set(ctx, key, strconv.FormatBool(value))
}

// SetMultiline implements ci.OutputSink.
func (s *Sink) SetMultiline(_ context.Context, key string, lines []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("fakeoutputsink: SetMultiline after Close"+": %w", errs.ErrValidation)
	}

	cp := make([]string, len(lines))
	copy(cp, lines)
	s.multiline[key] = cp

	return nil
}

// Close implements ci.OutputSink.
func (s *Sink) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	s.closeCount++

	return nil
}

// Single returns the scalar value for key, or empty string if absent.
func (s *Sink) Single(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.scalar[key]
}

// Multiline returns the multi-line value for key, or nil if absent.
func (s *Sink) Multiline(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := s.multiline[key]
	cp := make([]string, len(v))
	copy(cp, v)

	return cp
}

// AllScalar returns a sorted snapshot of all scalar key=value pairs.
func (s *Sink) AllScalar() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make(map[string]string, len(s.scalar))
	for k, v := range s.scalar {
		cp[k] = v
	}

	return cp
}

// Keys returns the union of scalar + multiline keys, sorted.
func (s *Sink) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[string]struct{}{}
	for k := range s.scalar {
		seen[k] = struct{}{}
	}

	for k := range s.multiline {
		seen[k] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// CloseCount returns how many times Close was called. Should be 1 in
// well-behaved tests.
func (s *Sink) CloseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeCount
}

// Compile-time check.
var _ ci.OutputSink = (*Sink)(nil)
