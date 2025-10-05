// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestCheckoutAssertionEnv_IsClosed(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT", "GITHUB_TOKEN"} {
		t.Setenv(name, "synthetic-canary")
	}
	root := t.TempDir()
	home := t.TempDir()
	cmd := checkoutGitCommand(t.Context(), root, home, "rev-parse", "HEAD")
	for _, entry := range cmd.Env {
		require.NotContains(t, entry, "synthetic-canary")
	}
	require.Contains(t, cmd.Env, "HOME="+home)
	require.Contains(t, cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null")
	require.Contains(t, strings.Join(cmd.Args, " "), "-C "+root)
}
