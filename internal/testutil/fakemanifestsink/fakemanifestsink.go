// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package fakemanifestsink is an in-memory implementation of
// ci.ManifestSink for app-layer tests. Each Write/WriteJSON records
// the (stage, body) pair; tests inspect via Body / Calls.
package fakemanifestsink

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// Sink implements ci.ManifestSink. Safe for concurrent use.
type Sink struct {
	t       *testing.T
	mu      sync.Mutex
	writes  map[string]string // stage → JSON body (raw)
	allKeys []string          // insertion order
}

// New returns a fresh Sink.
func New(t *testing.T) *Sink {
	t.Helper()

	return &Sink{t: t, writes: map[string]string{}}
}

// Write implements ci.ManifestSink. Marshals result via encoding/json
// (alphabetical key order — callers that need declaration order use
// WriteJSON).
func (s *Sink) Write(_ context.Context, stage string, result map[string]any) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}

	s.record(stage, string(body))

	return nil
}

// WriteJSON implements ci.ManifestSink.
func (s *Sink) WriteJSON(_ context.Context, stage string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	raw, err := body.MarshalJSON()
	if err != nil {
		return err
	}

	s.record(stage, string(raw))

	return nil
}

// Body returns the most recent JSON body written for stage, or "".
func (s *Sink) Body(stage string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writes[stage]
}

// Stages returns every stage name written, in insertion order.
func (s *Sink) Stages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, len(s.allKeys))
	copy(out, s.allKeys)

	return out
}

func (s *Sink) record(stage, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.writes[stage]; !ok {
		s.allKeys = append(s.allKeys, stage)
	}

	s.writes[stage] = body
}

// Compile-time conformance check.
var _ ci.ManifestSink = (*Sink)(nil)
