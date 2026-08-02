// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package planfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
)

// resolveFlag runs a one-flag command and returns what the flag resolved to.
func resolveFlag(t *testing.T, flag cli.Flag, args ...string) string {
	t.Helper()

	var got string

	cmd := &cli.Command{
		Name:  "test",
		Flags: []cli.Flag{flag},
		Action: func(_ context.Context, cmd *cli.Command) error {
			got = cmd.String("context")

			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, args...)))

	return got
}

func writePlan(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv(planfile.EnvVar, path)
}

func TestPlanPrecedence(t *testing.T) {
	newFlag := func() cli.Flag {
		return &cli.StringFlag{
			Name:    "context",
			Value:   "default-value",
			Sources: planfile.Vars("container build", "context", "BUILD_CONTEXT"),
		}
	}

	t.Run("flag beats plan and env", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": "from-plan"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-flag", resolveFlag(t, newFlag(), "--context", "from-flag"))
	})

	t.Run("plan beats env", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": "from-plan"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-plan", resolveFlag(t, newFlag()))
	})

	t.Run("env wins when plan lacks the key", func(t *testing.T) {
		writePlan(t, `{"container build": {"other": "x"}}`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-env", resolveFlag(t, newFlag()))
	})

	t.Run("default when nothing is set", func(t *testing.T) {
		require.Equal(t, "default-value", resolveFlag(t, newFlag()))
	})

	t.Run("scalar json values are stringified", func(t *testing.T) {
		writePlan(t, `{"container build": {"context": 42}}`)
		require.Equal(t, "42", resolveFlag(t, newFlag()))
	})

	t.Run("malformed plan is treated as absent", func(t *testing.T) {
		writePlan(t, `{not json`)
		t.Setenv("BUILD_CONTEXT", "from-env")
		require.Equal(t, "from-env", resolveFlag(t, newFlag()))
	})
}
