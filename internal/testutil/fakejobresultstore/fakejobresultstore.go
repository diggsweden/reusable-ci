// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package fakejobresultstore is an in-memory implementation of
// ci.JobResultStore for app-layer tests. WriteJob records the raw JSON per
// job; CollectJobs returns every recorded document. Seed lets a test preload
// records as if prior jobs had written them.
package fakejobresultstore

import (
	"context"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
)

// Store implements ci.JobResultStore. Safe for concurrent use.
type Store struct {
	t    *testing.T
	mu   sync.Mutex
	docs map[string][]byte // job → raw JSON
	keys []string          // insertion order
}

// New returns a fresh Store.
func New(t *testing.T) *Store {
	t.Helper()

	return &Store{t: t, docs: map[string][]byte{}}
}

// Seed preloads a raw job-result document under job, as if a prior job had
// recorded it. Returns the Store for chaining.
func (s *Store) Seed(job, doc string) *Store {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.docs[job]; !ok {
		s.keys = append(s.keys, job)
	}

	s.docs[job] = []byte(doc)

	return s
}

// WriteJob implements ci.JobResultStore.
func (s *Store) WriteJob(_ context.Context, job string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	raw, err := body.MarshalJSON()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.docs[job]; !ok {
		s.keys = append(s.keys, job)
	}

	s.docs[job] = raw

	return nil
}

// CollectJobs implements ci.JobResultStore, returning records in insertion
// order for deterministic tests.
func (s *Store) CollectJobs(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([][]byte, 0, len(s.keys))
	for _, job := range s.keys {
		out = append(out, s.docs[job])
	}

	return out, nil
}

// Body returns the most recent raw document recorded for job, or "".
func (s *Store) Body(job string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return string(s.docs[job])
}

// Compile-time conformance check.
var _ ci.JobResultStore = (*Store)(nil)
