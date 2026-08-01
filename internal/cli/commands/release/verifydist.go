// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
)

// verifyDistCmd is the Go port of forgejo-ci's verify-dist.sh: a
// cross-job-boundary integrity check that the dist/ tree handed from the
// (keyless) build job to the signing job is structurally safe and
// byte-identical to what was built — refusing to sign tampered artifacts.
func verifyDistCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-dist",
		Usage: "verify a dist/ tree is structurally safe and matches an expected digest (cross-job integrity)",
		Description: `Rejects a dist/ that is a symlink, contains symlinks, contains
non-regular entries, or has control characters in any path, then
recomputes its canonical digest and fails on mismatch. The digest
algorithm is byte-compatible with forgejo-ci's dist-digest.sh.

EXAMPLE:
   reusable-ci release verify-dist --dist-dir dist --expected-digest sha256:abc...`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dist-dir", Value: "dist", Usage: "directory to verify"},
			&cli.StringFlag{Name: "expected-digest", Sources: cli.EnvVars("EXPECTED_DIGEST", "DIST_DIGEST"), Usage: "expected dist digest (from the build job)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			dir := cmd.String("dist-dir")
			if err := apprelease.VerifyDist(dir, cmd.String("expected-digest")); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "dist/ integrity verified: %s\n", dir)

			return nil
		},
	}
}
