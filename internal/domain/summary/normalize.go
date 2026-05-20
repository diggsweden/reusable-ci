// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package summary holds pure helpers for stage-result composition,
// status normalisation, and step-summary fragment building. The
// composers in internal/app/summary build on these primitives.
package summary

// Result is a normalised CI result value.
type Result string

// Recognised Result values.
const (
	ResultSuccess   Result = "success"
	ResultFailure   Result = "failure"
	ResultCancelled Result = "cancelled"
	ResultSkipped   Result = "skipped"
)

// NormalizeResult maps any string to one of the four canonical
// results. Unknown / empty values fall through to ResultSkipped.
func NormalizeResult(s string) Result {
	switch Result(s) {
	case ResultSuccess, ResultFailure, ResultCancelled, ResultSkipped:
		return Result(s)
	default:
		return ResultSkipped
	}
}

// NormalizeJobStatus maps a CI runner's own job-status string to a canonical
// Result, accepting both forge spellings: GitHub Actions `job.status`
// (success/failure/cancelled) and GitLab `$CI_JOB_STATUS`
// (success/failed/canceled).
//
// Unlike NormalizeResult, it is **fail-closed**: an unknown or empty status
// maps to ResultFailure, never ResultSkipped. A job records its own outcome,
// so an unrecognised value means the outcome could not be confirmed — treating
// that as success/skipped would silently hide a failed job from the stage gate.
// (A genuinely skipped job never runs its steps, so it writes no record at all;
// absence is handled by the stage plan, not by this function.)
func NormalizeJobStatus(s string) Result {
	switch s {
	case string(ResultSuccess):
		return ResultSuccess
	case string(ResultFailure), "failed":
		return ResultFailure
	case string(ResultCancelled), "canceled":
		return ResultCancelled
	case string(ResultSkipped):
		return ResultSkipped
	default:
		return ResultFailure
	}
}

// IsResult reports whether r is one of the canonical CI result values.
func IsResult(r Result) bool {
	switch r {
	case ResultSuccess, ResultFailure, ResultCancelled, ResultSkipped:
		return true
	default:
		return false
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
