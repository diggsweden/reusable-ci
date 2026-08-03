// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestGroupsFailClosedWithoutASubcommand asserts that every group command
// (one carrying subcommands) refuses to run bare, with a usage error → exit 2.
//
// urfave/cli's default is to print help and exit 0, which is fail-OPEN: a
// workflow step that lost its last path segment (`reusable-ci validate tag`
// where `validate tag signature` was meant) would pass green having validated
// nothing. Nothing else in the guard family catches that — the verb path still
// exists, so forgejo-ci's test-cli-invocation-contract.sh is satisfied, and the
// surface is unchanged, so the reference-sync and help guards are satisfied. It
// is a runtime behaviour, and every other check here inspects surface facts.
//
// Walking the live tree means a newly added group inherits the invariant
// automatically: forget to give it subcommands-or-an-Action and this fails.
func TestGroupsFailClosedWithoutASubcommand(t *testing.T) {
	t.Parallel()

	root := cli.New(cli.BuildInfo{Version: "dev"})

	var offenders []string

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}

		// Leaves are the arg-guard's business (see applyArgGuards), not this one.
		if len(cmd.Commands) == 0 {
			return
		}

		if cmd.Action == nil {
			offenders = append(offenders,
				path+": group has no Action — urfave/cli would print help and exit 0 (fail-open)")

			return
		}

		err := cmd.Action(context.Background(), cmd)
		if !errors.Is(err, errs.ErrUsage) {
			offenders = append(offenders, fmt.Sprintf(
				"%s: bare group returned %v — want an errs.ErrUsage (exit 2)", path, err))
		}
	}

	for _, sub := range root.Commands {
		walk(sub.Name, sub)
	}

	require.Emptyf(t, offenders, "groups that do not fail closed:\n%s", strings.Join(offenders, "\n"))
}

// TestBareGroupErrorNamesItsSubcommands asserts the fail-closed error is
// actionable rather than a bare refusal: it must list what the caller could
// have typed. A CI log line is often all the operator gets.
func TestBareGroupErrorNamesItsSubcommands(t *testing.T) {
	t.Parallel()

	root := cli.New(cli.BuildInfo{Version: "dev"})

	var ledger *urfavecli.Command

	for _, group := range root.Commands {
		if group.Name != "container" {
			continue
		}

		for _, sub := range group.Commands {
			if sub.Name == "ledger" {
				ledger = sub
			}
		}
	}

	require.NotNil(t, ledger, "container ledger not found — rename?")

	err := ledger.Action(context.Background(), ledger)
	require.ErrorIs(t, err, errs.ErrUsage)
	require.Contains(t, err.Error(), "requires a subcommand")
	require.Contains(t, err.Error(), "promote", "the error should name the available subcommands")
	require.NotContains(t, err.Error(), "help", "the built-in help entry is not a real operation")
}

// TestRootStillRunsBare pins the deliberate exemption: `reusable-ci` with no
// arguments is the discovery path and must keep printing help at exit 0. The
// failure mode the parent guard exists for is a command that lost its *last*
// segment, not one that lost every segment.
func TestRootStillRunsBare(t *testing.T) {
	t.Parallel()

	root := cli.New(cli.BuildInfo{Version: "dev"})

	require.Nil(t, root.Action, "root must stay actionless so urfave/cli renders help at exit 0")
}
