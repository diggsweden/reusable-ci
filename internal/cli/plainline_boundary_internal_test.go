// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"log/slog"
	"testing"
	"time"
)

func TestPlainLineBoundary_EscapesCRAndLF(t *testing.T) {
	t.Parallel()

	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		prefix := ""
		if level == slog.LevelWarn {
			prefix = "warning: "
		}

		if level == slog.LevelError {
			prefix = "error: "
		}

		for _, tc := range []struct{ input, want string }{{"first\nsecond", `first\nsecond`}, {"first\rsecond", `first\rsecond`}, {"first\r\nsecond", `first\r\nsecond`}, {"normal", "normal"}} {
			var out bytes.Buffer

			err := newPlainHandler(&out, level).Handle(t.Context(), slog.NewRecord(time.Time{}, level, tc.input, 0))
			require.NoError(t, err)
			require.Equal(t, prefix+tc.want+"\n", out.String())
		}
	}
}
