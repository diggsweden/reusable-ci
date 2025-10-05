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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// Sink writes <stage>-result.json under Dir. Safe for concurrent use —
// each Write / WriteJSON is guarded by mu so racing callers don't
// interleave single-file staging and installation. Matches the
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
	if !summary.ValidResultName(stage) {
		return fmt.Errorf("invalid manifest stage name %q: %w", stage, errs.ErrUsage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal manifest %q: %w", stage, err)
	}

	return writeResultFile(s.Dir, stage+"-result.json", body)
}

// WriteJSON implements ci.ManifestSink. The body's MarshalJSON output
// is written verbatim — callers that want the bash's declaration-order
// keys pass a value with a custom MarshalJSON (e.g.
// summary.StageResultEnvelope).
func (s *Sink) WriteJSON(_ context.Context, stage string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	if !summary.ValidResultName(stage) {
		return fmt.Errorf("invalid manifest stage name %q: %w", stage, errs.ErrUsage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := body.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal manifest %q: %w", stage, err)
	}

	return writeResultFile(s.Dir, stage+"-result.json", raw)
}

// jobsSubdir holds per-job result records, kept separate from the
// <stage>-result.json manifests in Dir so the two are globbed independently.
const jobsSubdir = "jobs"

// WriteJob implements ci.JobResultStore. Writes one job's record to
// <Dir>/jobs/<job>.json. The body's MarshalJSON output is written verbatim
// (callers pass summary.JobResultEnvelope for canonical key order).
func (s *Sink) WriteJob(_ context.Context, job string, body interface {
	MarshalJSON() ([]byte, error)
}) error {
	if !summary.ValidResultName(job) {
		return fmt.Errorf("invalid job-result name %q: %w", job, errs.ErrUsage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := body.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal job-result %q: %w", job, err)
	}

	return writeResultFile(filepath.Join(s.Dir, jobsSubdir), job+".json", raw)
}

// Stage only this record under a checked real directory. Installation rejects
// preexisting symlinks and replaces regular files without truncating hardlinks.
func writeResultFile(dir, name string, body []byte) error {
	stage, err := pathsafe.NewArtifactStaging(dir)
	if err != nil {
		return err
	}
	defer func() { _ = stage.Close() }()

	destination, err := pathsafe.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()

	previous, err := destination.Lstat(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect result %q: %w", name, err)
	}

	if err := stage.Root().WriteFile(name, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("stage result %q: %w", name, err)
	}
	// Preserve replacement permissions on the new inode, never on an old
	// inode shared with a hardlink. New files retain the creation umask.
	if previous != nil && previous.Mode().IsRegular() {
		if err := stage.Root().Chmod(name, previous.Mode().Perm()); err != nil {
			return fmt.Errorf("preserve result permissions %q: %w", name, err)
		}
	}

	if err := stage.Install(); err != nil {
		return fmt.Errorf("install result %q: %w", name, err)
	}

	return nil
}

// CollectJobs implements ci.JobResultStore. Reads every <Dir>/jobs/*.json as
// raw JSON, using the shared bounded regular-file reader and rejecting existing
// symlinks. A missing jobs directory yields no records (not an error): a stage
// where no job ran simply collected nothing.
func (s *Sink) CollectJobs(_ context.Context) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.Dir, jobsSubdir)

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("read job-result dir %q: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("list job-result dir %q: %w", dir, err)
	}

	docs := make([][]byte, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		info, err := root.Lstat(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("inspect job-result %q: %w", entry.Name(), err)
		}

		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("job-result %q is not a regular file: %w", entry.Name(), errs.ErrValidation)
		}

		data, err := cliio.ReadFileInRoot(root, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read job-result %q: %w", entry.Name(), err)
		}

		docs = append(docs, data)
	}

	return docs, nil
}

// Compile-time conformance checks.
var (
	_ ci.ManifestSink   = (*Sink)(nil)
	_ ci.JobResultStore = (*Sink)(nil)
)
