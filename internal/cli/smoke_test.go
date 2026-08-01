// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	urfavecli "github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// TestAllCommands_HelpRenders is the local-first smoke suite: with no CI
// environment, every command in the tree must render its --help without
// error or panic. --help short-circuits before any forge/deps wiring, so
// this is a pure "the command tree is well-formed and every verb
// self-documents" guarantee — it catches a malformed flag, a nil action
// node, or a broken subgroup the moment it is introduced.
func TestAllCommands_HelpRenders(t *testing.T) {
	t.Parallel()

	for _, path := range allCommandPaths() {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			t.Parallel()

			root := cli.New(cli.BuildInfo{Version: "dev"})
			root.Writer = io.Discard
			root.ErrWriter = io.Discard

			args := append(append([]string{"reusable-ci"}, path...), "--help")
			require.NoErrorf(t, root.Run(context.Background(), args), "help failed for %q", strings.Join(path, " "))
		})
	}
}

// allCommandPaths enumerates every command path (depth-first) under the
// root, e.g. ["container", "ledger", "promote"].
func allCommandPaths() [][]string {
	var paths [][]string

	var collect func(prefix []string, cmd *urfavecli.Command)

	collect = func(prefix []string, cmd *urfavecli.Command) {
		for _, child := range cmd.Commands {
			path := append(append([]string{}, prefix...), child.Name)
			paths = append(paths, path)
			collect(path, child)
		}
	}

	collect(nil, cli.New(cli.BuildInfo{Version: "dev"}))

	return paths
}
