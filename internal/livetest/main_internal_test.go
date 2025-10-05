// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"os"
	"strings"
	"testing"
)

// TestMain gives the package a well-formed runner digest when the process has
// none. Rendering a probe reads RC_LIVE_RUNNER_SHA256 and panics without one,
// so these offline tests used to pass only on a host that happened to export
// it, and the first panic hid every later failure in the package. Tests about
// the digest itself set their own value.
func TestMain(m *testing.M) {
	if os.Getenv(runnerBinarySHA256Env) == "" {
		_ = os.Setenv(runnerBinarySHA256Env, strings.Repeat("0", 64))
	}

	os.Exit(m.Run())
}
