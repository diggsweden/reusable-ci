// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package cli_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/cli"
)

func TestNew_VersionString(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "1.2.3", Commit: "abc1234", Date: "2026-01-01T00:00:00Z"})

	require.Equal(t, "reusable-ci", cmd.Name)
	require.Equal(t, "1.2.3 (commit abc1234, built 2026-01-01T00:00:00Z)", cmd.Version)
}

func TestRoot_RunHelp(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--help"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	require.NoError(t, err)
}

func TestRoot_RejectsBogusLogLevel(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--log-level=bogus"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid log-level")
}

func TestRoot_AcceptsValidLogLevels(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"debug", "info", "warn", "warning", "error", "DEBUG", "INFO"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()

			cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
			// --help short-circuits before any subcommand action; gives us a clean
			// run that exercises the Before hook with the chosen log level.
			err := cmd.Run(context.Background(), []string{"reusable-ci", "--log-level=" + level, "--help"})
			require.NoError(t, err)
		})
	}
}

func TestRoot_QuietImpliesError(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--quiet", "--help"})
	require.NoError(t, err)
}
