// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package summary holds pure helpers for stage-result composition,
// status normalisation, and step-summary fragment building. The
// composers in internal/app/summary build on these primitives.
package summary

// Result is a normalised CI result value.
type Result string

const (
	ResultSuccess   Result = "success"
	ResultFailure   Result = "failure"
	ResultCancelled Result = "cancelled"
	ResultSkipped   Result = "skipped"
)

// NormalizeResult maps any string to one of the four canonical results.
// Unknown / empty values fall through to ResultSkipped, matching the
// ci_normalize_result default in scripts/ci/output.sh.
func NormalizeResult(s string) Result {
	switch Result(s) {
	case ResultSuccess, ResultFailure, ResultCancelled, ResultSkipped:
		return Result(s)
	default:
		return ResultSkipped
	}
}

// StatusIcon returns the marker glyph used in step-summary tables.
// Mirrors ci_status_icon: success → ✓, skipped → −, anything else → ✗.
func StatusIcon(s string) string {
	switch s {
	case string(ResultSuccess):
		return "✓"
	case string(ResultSkipped):
		return "−"
	default:
		return "✗"
	}
}

// IconFor is a typed convenience over StatusIcon.
func IconFor(r Result) string { return StatusIcon(string(r)) }
