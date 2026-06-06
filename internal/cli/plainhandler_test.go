// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestPlainHandler_Rendering(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		msg   string
		want  string
	}{
		{"info has no prefix", slog.LevelInfo, "config loaded", "config loaded\n"},
		{"warn prefixed lowercase", slog.LevelWarn, "stat failed; skipping", "warning: stat failed; skipping\n"},
		{"error prefixed lowercase", slog.LevelError, "upload aborted", "error: upload aborted\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer

			h := newPlainHandler(&buf, slog.LevelInfo)
			logger := slog.New(h)

			switch tc.level {
			case slog.LevelInfo:
				logger.Info(tc.msg)
			case slog.LevelWarn:
				logger.Warn(tc.msg)
			case slog.LevelError:
				logger.Error(tc.msg)
			default:
				t.Fatalf("test fixture uses unsupported level %v", tc.level)
			}

			if buf.String() != tc.want {
				t.Errorf("got %q, want %q", buf.String(), tc.want)
			}
		})
	}
}

func TestPlainHandler_DropsAttrsAndGroups(t *testing.T) {
	// Attrs and groups are diagnostic context; they belong in the
	// structured handler installed at --log-level=debug. plainHandler
	// must not leak time=, level=, or key=value tokens.
	var buf bytes.Buffer

	logger := slog.New(newPlainHandler(&buf, slog.LevelInfo))

	logger.With("path", "/etc/passwd").WithGroup("ctx").Warn("denied", "user", "root")

	got := buf.String()
	if got != "warning: denied\n" {
		t.Fatalf("got %q, want %q", got, "warning: denied\n")
	}

	for _, banned := range []string{"time=", "level=", "path=", "user=", "ctx."} {
		if strings.Contains(got, banned) {
			t.Errorf("output %q must not contain %q", got, banned)
		}
	}
}

func TestPlainHandler_LevelGating(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPlainHandler(&buf, slog.LevelWarn))

	logger.Info("hidden")
	logger.Warn("shown")

	if got := buf.String(); got != "warning: shown\n" {
		t.Errorf("got %q, want only the warn line", got)
	}
}

func TestPlainHandler_ConcurrentWritesAreLineAtomic(t *testing.T) {
	// Sanity-check the mutex: under concurrent fanout no line should be
	// truncated or interleaved. We don't assert ordering — only that
	// every emitted line stays intact.
	var buf bytes.Buffer

	logger := slog.New(newPlainHandler(&buf, slog.LevelInfo))

	const (
		workers = 16
		each    = 32
	)

	var wg sync.WaitGroup
	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			for range each {
				logger.Warn("parallel record")
			}
		}()
	}

	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != workers*each {
		t.Fatalf("got %d lines, want %d", len(lines), workers*each)
	}

	for i, line := range lines {
		if line != "warning: parallel record" {
			t.Errorf("line %d corrupted: %q", i, line)
		}
	}
}

func TestPlainHandler_EnabledRespectsThreshold(t *testing.T) {
	h := newPlainHandler(&bytes.Buffer{}, slog.LevelWarn)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info should be disabled when threshold is Warn")
	}

	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Error should be enabled when threshold is Warn")
	}
}
