// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package ci defines the platform-portable output ports.
//
// GitHub uses a $GITHUB_OUTPUT adapter, GitLab uses a dotenv report adapter,
// and use cases never know which sink they're writing to.
package ci

import "context"

// OutputSink receives the per-step scalar / multi-line outputs that
// downstream workflow steps consume. On GitHub Actions this is the
// $GITHUB_OUTPUT file; on GitLab CI it's an artifacts:reports:dotenv file.
//
// Multi-line outputs only have a stable encoding on GHA (heredoc); GitLab
// callers should fall back to the manifest file via ManifestSink.
type OutputSink interface {
	// Set writes a single scalar output: key=value.
	Set(ctx context.Context, key, value string) error

	// SetBool writes a typed boolean output. String-formatted sinks
	// (GHA, GitLab dotenv) emit "true" / "false" via strconv.FormatBool;
	// structured sinks (JSON) preserve the boolean type so consumers
	// can branch with `value === true` instead of string comparison.
	SetBool(ctx context.Context, key string, value bool) error

	// SetMultiline writes a multi-line value. On GitLab the implementation
	// returns ErrUnsupported and callers should switch to a manifest file.
	SetMultiline(ctx context.Context, key string, lines []string) error

	// Close flushes any buffered state and releases handles.
	// Calling Set/SetMultiline after Close returns an error.
	Close(ctx context.Context) error
}

// SummarySink receives markdown content for the per-step summary
// ($GITHUB_STEP_SUMMARY on GHA, an artifact markdown file on GitLab).
type SummarySink interface {
	Append(ctx context.Context, markdown string) error
}

// ManifestSink writes a structured stage-result manifest to
// $CI_RESULTS_DIR/<stage>-result.json. Cross-job consumption uses this
// instead of OutputSink for portability.
//
// Two write paths:
//
//	Write     — convenience for callers with a map[string]any. Go's
//	            encoding/json sorts keys alphabetically; callers that
//	            care about key order should use WriteJSON instead.
//	WriteJSON — accepts a json.Marshaler so callers preserve their
//	            own key ordering (used by stage-result envelopes that
//	            mirror the bash's emission sequence).
type ManifestSink interface {
	Write(ctx context.Context, stage string, result map[string]any) error
	WriteJSON(ctx context.Context, stage string, body interface {
		MarshalJSON() ([]byte, error)
	}) error
}
