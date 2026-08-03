// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appartifact "github.com/diggsweden/reusable-ci/v3/internal/app/artifact"
)

// digestCmd wires `reusable-ci artifact digest`. It needs no forge provider;
// it is a local, reproducible computation. The build->sign hand-off uses
// `release dist-digest` / `release validate-dist`, not this command.
func digestCmd() *cli.Command {
	return &cli.Command{
		Name:  "digest",
		Usage: "print a canonical, reproducible content digest of a directory",
		Description: `Computes a runner- and OS-independent content digest over the regular files
in a directory, committing to each file's path, execute bit, size, and contents.

For the build->sign hand-off, use "release dist-digest" and "release
validate-dist" instead: that pair is what the signing flow verifies against, and
its digest is byte-compatible with the shell implementation it replaced.

EXAMPLE (as a workflow step):
   DIGEST="$(reusable-ci artifact digest --dir ./dist)"`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagDir, Required: true, Sources: cli.EnvVars("ARTIFACT_DIR"), Usage: "directory whose contents are digested"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appartifact.Digest(os.Stdout, appartifact.DigestInput{Dir: cmd.String(flagDir)})
		},
	}
}
