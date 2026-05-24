// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// plainHandler is a [slog.Handler] that emits one undecorated line per
// record, suitable for clig.dev's "don't treat stderr like a log file"
// rule. It drops timestamps, level=… key/value tags, and all attrs.
//
// Rendering:
//
//	Info  → "<msg>"
//	Warn  → "warning: <msg>"
//	Error → "error: <msg>"
//
// Use this at log-level info/warn/error. For --log-level=debug the
// root installs [slog.TextHandler] instead, so operators piping stderr
// to a file still get structured records with timestamps and attrs.
type plainHandler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Level
}

func newPlainHandler(w io.Writer, level slog.Level) *plainHandler {
	return &plainHandler{w: w, level: level}
}

func (h *plainHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *plainHandler) Handle(_ context.Context, r slog.Record) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	var prefix string

	switch {
	case r.Level >= slog.LevelError:
		prefix = "error: "
	case r.Level >= slog.LevelWarn:
		prefix = "warning: "
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	_, err := fmt.Fprintln(h.w, prefix+r.Message)

	return err
}

// WithAttrs and WithGroup are no-ops: plainHandler drops contextual
// data at non-debug levels by design. Attrs added via slog.With are
// only meaningful for the structured handler installed at --log-level=debug.
func (h *plainHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *plainHandler) WithGroup(_ string) slog.Handler      { return h }
