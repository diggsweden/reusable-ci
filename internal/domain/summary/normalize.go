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
