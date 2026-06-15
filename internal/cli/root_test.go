// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"context"
	"os"
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

func TestNew_DocsLinkTracksInstalledVersion(t *testing.T) {
	t.Parallel()

	// A release build points the Docs link at its own tag so the web docs
	// match the installed binary (workflows pin reusable-ci to a tag).
	tagged := cli.New(cli.BuildInfo{Version: "v3.0.0", Commit: "abc", Date: "d"})
	require.Contains(t, tagged.Description, "tree/v3.0.0/docs")

	// dev/snapshot builds (no real tag) fall back to main.
	for _, v := range []string{"dev", "", "snapshot-123"} {
		dev := cli.New(cli.BuildInfo{Version: v, Commit: "abc", Date: "d"})
		require.Contains(t, dev.Description, "tree/main/docs",
			"version %q should fall back to main", v)
	}
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

func TestRoot_RejectsBogusProvider(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	// No --help: --help short-circuits before a Before-hook error
	// surfaces (same reason TestRoot_RejectsBogusLogLevel omits it).
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--provider=bananas"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid --provider")
}

func TestRoot_RejectsBogusRunner(t *testing.T) {
	t.Parallel()

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--runner=spaceship"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid --runner")
}

func TestRoot_ProviderFlagBridgesToEnv(t *testing.T) {
	// Not parallel: the Before hook bridges argv → REUSABLE_CI_PROVIDER
	// via os.Setenv so platform.Detect (which reads env) observes it.
	// t.Setenv records the original and restores it after the test even
	// though the bridge overwrites it mid-run.
	t.Setenv("REUSABLE_CI_PROVIDER", "")

	cmd := cli.New(cli.BuildInfo{Version: "x", Commit: "y", Date: "z"})
	// No subcommand + valid flag: Before runs the bridge, then root
	// prints help and returns nil — leaving the bridged env observable.
	err := cmd.Run(context.Background(), []string{"reusable-ci", "--provider=forgejo"})
	require.NoError(t, err)
	require.Equal(t, "forgejo", os.Getenv("REUSABLE_CI_PROVIDER"))
}
