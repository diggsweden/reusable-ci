// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package log holds the project's logging conventions on top of
// log/slog. slog handles the actual writing; this package adds:
//
//   - LevelTrace: one notch below slog.LevelDebug, for very-fine-
//     grained traces (per-entry walk records, per-cell config parses,
//     every shell command's exact argv) that would drown a normal
//     debug session.
//
//   - Trace / TraceContext helpers that mirror slog.Debug / DebugContext.
//
//   - ReplaceLevelAttr — a slog.HandlerOptions.ReplaceAttr hook that
//     renders LevelTrace as the string "TRACE" instead of "DEBUG-4".
//
// Two-tier rule of thumb:
//
//   - Trace: facts about every input the code touched. Loud, noisy,
//     intended for "why does this one entry behave wrong" sessions.
//   - Debug: step-level. Each decision, each fork, each subprocess
//     invocation — but not the contents of each iteration.
//
// Use Trace only behind a level check (via slog.Default().Enabled) when
// the message is expensive to format.
package log

import (
	"context"
	"log/slog"
)

// LevelTrace is the project-defined sub-debug level. slog accepts any
// int as a level; we pick -8 (debug is -4) so handlers that compare
// numerically order Trace < Debug correctly.
const LevelTrace slog.Level = slog.LevelDebug - 4

// Trace logs at LevelTrace via the default slog logger. Mirrors the
// slog.Debug shape.
func Trace(msg string, args ...any) {
	slog.Log(context.Background(), LevelTrace, msg, args...)
}

// TraceContext is the context-aware sibling of Trace.
func TraceContext(ctx context.Context, msg string, args ...any) {
	slog.Log(ctx, LevelTrace, msg, args...)
}

// ReplaceLevelAttr is a slog.HandlerOptions.ReplaceAttr value that
// renders our custom levels with friendly names. It leaves the standard
// levels (DEBUG/INFO/WARN/ERROR) alone so handler output stays familiar.
//
// Wire it into the default handler:
//
//	slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
//	    Level:       level,
//	    ReplaceAttr: log.ReplaceLevelAttr,
//	})
func ReplaceLevelAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) != 0 || a.Key != slog.LevelKey {
		return a
	}
	lvl, ok := a.Value.Any().(slog.Level)
	if !ok {
		return a
	}
	if lvl == LevelTrace {
		a.Value = slog.StringValue("TRACE")
	}
	return a
}
