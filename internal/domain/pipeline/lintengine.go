// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// LintEngine is the general-purpose lint engine a pull request runs. The
// engines are mutually exclusive — running two would duplicate SAST findings
// in Code Scanning and double the CI cost — so the choice is one value, not a
// set of toggles. Swift format/lint is orthogonal and layered on top of
// whichever engine runs.
type LintEngine string

// Recognised LintEngine values. none is the default: a forge- and
// consumer-neutral engine ships no particular linter as its out-of-box choice.
// A consumer selects nanolinter (fast, node-less) or megalinter (heavier,
// governance-recognised) explicitly; none disables general linting entirely.
const (
	LintEngineNanolinter LintEngine = "nanolinter"
	LintEngineMegalinter LintEngine = "megalinter"
	LintEngineNone       LintEngine = "none"
)

// ParseLintEngine normalises (case- and whitespace-insensitive) and validates a
// lint-engine string. Empty defaults to none — the engine must not ship one
// consumer's linter as its default, so a consumer opts into nanolinter or
// megalinter explicitly; any other unrecognised value is a usage error.
func ParseLintEngine(raw string) (LintEngine, error) {
	switch engine := LintEngine(strings.ToLower(strings.TrimSpace(raw))); engine {
	case "":
		return LintEngineNone, nil
	case LintEngineNanolinter, LintEngineMegalinter, LintEngineNone:
		return engine, nil
	default:
		return "", fmt.Errorf("unknown lint-engine %q (want nanolinter, megalinter, or none): %w", raw, errs.ErrUsage)
	}
}
