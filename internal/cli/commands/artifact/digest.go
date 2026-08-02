// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	appartifact "github.com/diggsweden/reusable-ci/v3/internal/app/artifact"
)

// digestCmd wires `reusable-ci artifact digest` — the build->sign tamper-evidence
// primitive. It needs no forge provider; it is a local, reproducible computation.
func digestCmd() *cli.Command {
	return &cli.Command{
		Name:  "digest",
		Usage: "print a canonical, reproducible content digest of a directory (build->sign tamper-evidence)",
		Description: `Computes a runner- and OS-independent content digest over the regular files
in a directory. The producing job records it; the signing job re-derives it and
aborts on mismatch, so an intervening job cannot tamper with the artifact.

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
