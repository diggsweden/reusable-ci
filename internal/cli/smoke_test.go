// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"bytes"
	"context"
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
//
// Each path writes to its own buffers and must print its own page. Output used
// to go to io.Discard with only the error checked, so a command whose --help
// fell back to its parent's page, or a walk that found no commands at all,
// passed.
func TestAllCommands_HelpRenders(t *testing.T) {
	t.Parallel()

	paths := allCommandPaths()

	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, strings.Join(path, " "))
	}

	for _, known := range []string{"build maven run", "container ledger merge", "sbom assemble", "version"} {
		require.Containsf(t, names, known, "the command walk did not reach %q", known)
	}

	for _, path := range paths {
		name := strings.Join(path, " ")

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			root := cli.New(cli.BuildInfo{Version: "dev"})
			root.Writer = &stdout
			root.ErrWriter = &stderr

			args := append(append([]string{"reusable-ci"}, path...), "--help")
			require.NoErrorf(t, root.Run(context.Background(), args), "help failed for %q", name)

			// The NAME line is the command's full path followed by its usage
			// or the end of the line; a parent's page names the parent.
			page := stdout.String()

			heading := "NAME:\n   reusable-ci " + name
			if !strings.HasPrefix(page, heading+" - ") && !strings.HasPrefix(page, heading+"\n") {
				t.Errorf("help page does not start with %q:\n%s", heading, page)
			}

			if !strings.Contains(page, "USAGE:\n   reusable-ci "+name+" ") {
				t.Errorf("help page has no usage line for %q:\n%s", name, page)
			}

			if stderr.Len() != 0 {
				t.Errorf("help wrote to stderr: %q", stderr.String())
			}
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
