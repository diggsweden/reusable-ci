// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	domainlog "github.com/diggsweden/reusable-ci/internal/domain/log"
)

func newCapturingHandler(t *testing.T, level slog.Level) (*bytes.Buffer, func()) {
	t.Helper()
	prev := slog.Default()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: domainlog.ReplaceLevelAttr,
	})
	slog.SetDefault(slog.New(h))
	return &buf, func() { slog.SetDefault(prev) }
}

func TestTrace_EmitsWhenEnabled(t *testing.T) {
	buf, restore := newCapturingHandler(t, domainlog.LevelTrace)
	defer restore()

	domainlog.Trace("walked entry", "path", "/workspace/x")

	out := buf.String()
	require.Contains(t, out, "level=TRACE")
	require.Contains(t, out, "msg=\"walked entry\"")
	require.Contains(t, out, "path=/workspace/x")
}

func TestTrace_SuppressedAtDebugLevel(t *testing.T) {
	buf, restore := newCapturingHandler(t, slog.LevelDebug)
	defer restore()

	domainlog.Trace("should not appear")
	require.Empty(t, strings.TrimSpace(buf.String()))
}

func TestTraceContext_PassesContext(t *testing.T) {
	buf, restore := newCapturingHandler(t, domainlog.LevelTrace)
	defer restore()

	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("x"), "y")
	domainlog.TraceContext(ctx, "ctx trace", "k", 1)

	require.Contains(t, buf.String(), "level=TRACE")
	require.Contains(t, buf.String(), "k=1")
}

func TestReplaceLevelAttr_PreservesStandardLevels(t *testing.T) {
	buf, restore := newCapturingHandler(t, slog.LevelDebug)
	defer restore()

	slog.Info("hello")
	require.Contains(t, buf.String(), "level=INFO")
}
