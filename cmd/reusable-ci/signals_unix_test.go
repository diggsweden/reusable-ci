//go:build unix

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Only Unix supports sending these signals through Process.Signal. Both
// signal stages stay in owned children: the second can exit the process, and
// the first leaves its watcher armed until the process exits.
func TestSignalBoundary_OwnedChild(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
		for _, tc := range []struct {
			mode string
			code int
			want string
		}{
			{"cancel", 2, "Error: context canceled\n"},
			{"success", 0, ""},
			{"force", 130, "Force-quit.\n"},
		} {
			t.Run(sig.String()+"/"+tc.mode, func(t *testing.T) {
				stderr := runDiagnosticChild(t, tc.mode, sig, tc.code)
				require.Equal(t, "\ninterrupted; send another signal to force-quit.\n"+tc.want, stderr)
			})
		}
	}
}
