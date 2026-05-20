// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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

// JobResultStore persists and collects per-job outcome records — the
// forge-neutral replacement for GitHub Actions' `toJson(needs)`. Each job
// writes its own outcome as its last (always-run) step; a downstream summary
// job collects every record and aggregates the stage result from them. This
// works identically on GitHub, Forgejo, and GitLab, where no job can read a
// sibling's status: the records travel as ordinary run artifacts and live at
// $CI_RESULTS_DIR/jobs/<job>.json, kept separate from the <stage>-result.json
// manifests so the two are globbed independently.
//
// CollectJobs returns the raw JSON documents (not parsed): validation/parsing
// belongs to the domain (summary.ParseJobResultEnvelope), so this port stays
// free of summary types.
type JobResultStore interface {
	// WriteJob persists one job's outcome under jobs/<job>.json.
	WriteJob(ctx context.Context, job string, body interface {
		MarshalJSON() ([]byte, error)
	}) error

	// CollectJobs reads back every persisted job record as raw JSON.
	// A missing jobs directory is not an error — it yields no records.
	CollectJobs(ctx context.Context) ([][]byte, error)
}
