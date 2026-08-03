// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"fmt"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
)

// retiredVerbs are leading command verbs ADR 0003 retired in favour of a
// single sanctioned spelling. "verify" folds into "validate": asserting that
// something is trustworthy is a validate operation; the cryptographic nature
// is the noun (validate container-signature), not the verb.

// TestNoRetiredCommandVerbs keeps the CLI verb vocabulary coherent (ADR 0003):
// once a verb is retired, no command may reintroduce it as its primary name.
// New commands that reach for "verify" fail here with the sanctioned
// replacement, so the surface cannot silently re-diverge the way it did before
// the lexicon was written down.
func TestNoRetiredCommandVerbs(t *testing.T) {
	t.Parallel()

	retiredVerbs := map[string]string{
		"verify": "validate",
	}

	root := cli.New(cli.BuildInfo{Version: "dev"})

	var offenders []string

	var walk func(path string, cmd *urfavecli.Command)

	walk = func(path string, cmd *urfavecli.Command) {
		verb := cmd.Name
		if i := strings.IndexByte(verb, '-'); i >= 0 {
			verb = verb[:i]
		}

		if replacement, retired := retiredVerbs[verb]; retired {
			offenders = append(offenders, fmt.Sprintf(
				"%s: command verb %q is retired — rename to %q; see docs/adr/0003-cli-verb-lexicon.md",
				path, verb, replacement))
		}

		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}

	for _, sub := range root.Commands {
		walk(sub.Name, sub)
	}

	require.Emptyf(t, offenders, "retired command verbs in the tree:\n%s", strings.Join(offenders, "\n"))
}
