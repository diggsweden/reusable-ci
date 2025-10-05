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

// distDigestCmd exposes the release hand-off digest contract used by
// cross-job build-to-sign boundaries. It intentionally differs from `artifact
// digest`, which has a separate content-digest scheme.
func distDigestCmd() *cli.Command {
	return &cli.Command{
		Name:  "dist-digest",
		Usage: "print the current release dist/ hand-off digest",
		Description: `Computes the digest used by release validate-dist: the SHA-256 of
the sorted sha256sum manifest for every regular file under --dist-dir. Producer
and verifier use this canonical algorithm at the reusable-workflow hand-off.

Set --manifest-root to override the path prefix written into the manifest; use
--manifest-root . for artifact contents that are staged under different
directory names by producer and verifier.

EXAMPLE:
   reusable-ci release dist-digest --dist-dir dist`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagDistDir, Value: defaultDistDir, Usage: "directory whose release hand-off digest is printed"},
			&cli.StringFlag{Name: "manifest-root", Usage: "path prefix written into the sha256sum manifest (defaults to --dist-dir)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			digest, err := apprelease.DistDigestWithManifestRoot(cmd.String(flagDistDir), cmd.String("manifest-root"))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintln(os.Stdout, digest)

			return nil
		},
	}
}

// verifyDistCmd checks that a dist/ tree handed from the (keyless) build job to
// the signing job is structurally safe and byte-identical to what was built,
// refusing to sign tampered artifacts.
func verifyDistCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-dist",
		Usage: "validate a dist/ tree is structurally safe and matches an expected digest (cross-job integrity)",
		Description: `Rejects a dist/ that is a symlink, contains symlinks, contains
non-regular entries, or has control characters in any path, then
recomputes its canonical digest and fails on mismatch. The digest
algorithm is shared with release dist-digest.

Set --manifest-root to override the path prefix written into the manifest during
digest recomputation; use --manifest-root . for artifact contents that are
staged under different directory names by producer and verifier.

EXAMPLE:
   reusable-ci release validate-dist --dist-dir dist --expected-digest sha256:abc...`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagDistDir, Value: defaultDistDir, Usage: "directory to verify"},
			&cli.StringFlag{Name: "expected-digest", Sources: cli.EnvVars("EXPECTED_DIGEST", "DIST_DIGEST"), Usage: "expected dist digest (from the build job)"},
			&cli.StringFlag{Name: "manifest-root", Usage: "path prefix written into the sha256sum manifest during digest recomputation (defaults to --dist-dir)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			dir := cmd.String(flagDistDir)
			if err := apprelease.VerifyDistWithManifestRoot(dir, cmd.String("expected-digest"), cmd.String("manifest-root")); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "dist/ integrity verified: %s\n", dir)

			return nil
		},
	}
}
