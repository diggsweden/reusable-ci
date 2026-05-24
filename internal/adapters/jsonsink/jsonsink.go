// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package jsonsink implements ci.OutputSink as a JSON document on a
// caller-supplied io.Writer. Selected by the root --json / --format=json
// flag so command outputs reach stdout in a script-friendly shape
// instead of disappearing into a CI-specific output file.
//
// Buffering: Set / SetMultiline accumulate keys; Close emits a single
// JSON object (2-space indent, trailing newline) and releases the
// buffer. Calls after Close return an error so misuse fails loudly.
//
// Ordering: keys appear in insertion order to match the human-readable
// summary printed alongside, not alphabetical (encoding/json default).
// Tools that want stable diffs can pipe through `jq -S .`.
package jsonsink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// errSinkClosed is returned by Set/SetMultiline after Close.
var errSinkClosed = errors.New("output sink is closed")

// Sink buffers OutputSink writes and emits them as a single JSON
// object when Close is called. Safe for concurrent Set calls.
type Sink struct {
	w      io.Writer
	mu     sync.Mutex
	keys   []string                   // insertion order
	values map[string]json.RawMessage // key → value (scalar or string-array)
	closed bool
}

// New returns a Sink that writes its JSON document to w on Close. A
// nil writer is invalid — callers should pass os.Stdout for the
// standard --json case.
func New(w io.Writer) *Sink {
	return &Sink{
		w:      w,
		values: map[string]json.RawMessage{},
	}
}

// Set records a scalar key=value pair. Identical keys overwrite,
// matching ci.OutputSink semantics.
func (s *Sink) Set(_ context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("jsonsink: empty output key: %w", errs.ErrValidation)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("jsonsink: encode %q: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("jsonsink: Set: %w", errSinkClosed)
	}

	s.record(key, encoded)

	return nil
}

// SetBool records key with a native JSON boolean value. Consumers
// piping `--json` into `jq` can branch with `select(.is_snapshot)`
// instead of `select(.is_snapshot == "true")`.
func (s *Sink) SetBool(_ context.Context, key string, value bool) error {
	if key == "" {
		return fmt.Errorf("jsonsink: empty output key: %w", errs.ErrValidation)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("jsonsink: encode %q: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("jsonsink: SetBool: %w", errSinkClosed)
	}

	s.record(key, encoded)

	return nil
}

// SetMultiline records a key whose value is the joined lines string.
// JSON has no native heredoc — multi-line values become a single
// string with newline separators, mirroring how Set would behave if
// the caller passed strings.Join(lines, "\n").
func (s *Sink) SetMultiline(_ context.Context, key string, lines []string) error {
	if key == "" {
		return fmt.Errorf("jsonsink: empty output key: %w", errs.ErrValidation)
	}

	encoded, err := json.Marshal(strings.Join(lines, "\n"))
	if err != nil {
		return fmt.Errorf("jsonsink: encode %q: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("jsonsink: SetMultiline: %w", errSinkClosed)
	}

	s.record(key, encoded)

	return nil
}

// Close flushes the buffered keys as a single JSON object and marks the
// sink closed. Subsequent Set/SetMultiline calls fail. Idempotent.
func (s *Sink) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	if s.w == nil || len(s.keys) == 0 {
		return nil
	}

	if _, err := io.WriteString(s.w, "{\n"); err != nil {
		return fmt.Errorf("jsonsink: write: %w", err)
	}

	for i, key := range s.keys { //nolint:varnamelen // idiomatic loop index.
		// json.Marshal on a string cannot return a non-nil error per
		// encoding/json's documented contract; the assignment to err
		// here keeps the linter honest.
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return fmt.Errorf("jsonsink: encode key %q: %w", key, err)
		}

		sep := ",\n"
		if i == len(s.keys)-1 {
			sep = "\n"
		}

		if _, err := fmt.Fprintf(s.w, "  %s: %s%s", keyJSON, s.values[key], sep); err != nil {
			return fmt.Errorf("jsonsink: write: %w", err)
		}
	}

	if _, err := io.WriteString(s.w, "}\n"); err != nil {
		return fmt.Errorf("jsonsink: write: %w", err)
	}

	return nil
}

// record stores key in insertion order on first sight, then maps it to
// the encoded value. Repeated writes for the same key overwrite the
// value but preserve the original position.
func (s *Sink) record(key string, value json.RawMessage) {
	if _, seen := s.values[key]; !seen {
		s.keys = append(s.keys, key)
	}

	s.values[key] = value
}

// Compile-time check that *Sink implements the port.
var _ ci.OutputSink = (*Sink)(nil)
