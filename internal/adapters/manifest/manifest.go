// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package manifest implements ci.ManifestSink as a JSON-file
// writer rooted at $CI_RESULTS_DIR (default ".ci-results"). Each
// manifest goes to <stage>-result.json — the bash convention.
//
// Used by stage-result writers in app/summary so cross-job consumers
// have a portable file to read regardless of platform.
package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// Sink writes <stage>-result.json under Dir. Safe for concurrent use —
// each Write / WriteJSON is guarded by mu so racing callers don't
// interleave the MkdirAll + WriteFile sequence. Matches the
// concurrent-safety invariant the sibling ghaoutput / stepsummary sinks
// already establish.
type Sink struct {
	Dir string
	mu  sync.Mutex
}

// NewFromEnv reads $CI_RESULTS_DIR (default ".ci-results"). The
// directory is created on first Write.
func NewFromEnv() *Sink {
	dir := os.Getenv("CI_RESULTS_DIR")
	if dir == "" {
		dir = ".ci-results"
	}

	return &Sink{Dir: dir}
}

// New returns a Sink rooted at the given directory.
func New(dir string) *Sink { return &Sink{Dir: dir} }

// Write implements ci.ManifestSink. Marshals result to JSON and writes
// to <Dir>/<stage>-result.json. Marshalable values that don't already
// produce a deterministic shape (Go map iteration order) should be
// pre-shaped as a json.Marshaler — for stage-result envelopes that's
// summary.StageResultEnvelope.
func (s *Sink) Write(_ context.Context, stage string, result map[string]any) error {
	if stage == "" {
		return fmt.Errorf("manifest stage name is empty: %w", errs.ErrUsage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.Dir, 0o755); err != nil { //nolint:gosec // manifest dir read by downstream workflow steps.
		return fmt.Errorf("mkdir %q: %w", s.Dir, err)
	}

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal manifest %q: %w", stage, err)
	}

	path := filepath.Join(s.Dir, stage+"-result.json")
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil { //nolint:gosec // manifest file read by workflow; 0644 expected.
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}

// WriteJSON implements ci.ManifestSink. The body's MarshalJSON output
// is written verbatim — callers that want the bash's declaration-order
// keys pass a value with a custom MarshalJSON (e.g.
// summary.StageResultEnvelope).
func (s *Sink) WriteJSON(_ context.Context, stage string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	if stage == "" {
		return fmt.Errorf("manifest stage name is empty: %w", errs.ErrUsage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.Dir, 0o755); err != nil { //nolint:gosec // manifest dir read by downstream workflow steps.
		return fmt.Errorf("mkdir %q: %w", s.Dir, err)
	}

	raw, err := body.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal manifest %q: %w", stage, err)
	}

	path := filepath.Join(s.Dir, stage+"-result.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil { //nolint:gosec // manifest file read by workflow; 0644 expected.
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}

// Compile-time conformance check.
var _ ci.ManifestSink = (*Sink)(nil)
