// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func ledgerCleanupCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdCleanup,
		Usage: "delete each entry's staging candidate tag after verifying the promoted final tag (leaves candidates in place if unverified)",
		Description: `EXAMPLE:
   reusable-ci container ledger cleanup --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			ledgerAuthFileFlag("registry auth file for the promoted-tag verification"),
			expectedImageRepositoryFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				entries, err := ledgerEntriesFromCmd(cmd)
				if err != nil {
					return err
				}

				reg, err := cleanupReg(d, dryrun.Enabled(cmd), ledgerRegistry(cmd))
				if err != nil {
					return err
				}

				return runLedgerCleanup(ctx, reg, entries, cmd.String(flagTag), "ledger")
			})
		},
	}
}

// runLedgerCleanup is the shared cleanup body for `ledger cleanup` and
// `release-images cleanup`: delete each entry's staging candidate tag
// (the domain verifies the promoted final tag first), then report.
// msgPrefix names the calling boundary ("ledger" or "release images").
func runLedgerCleanup(ctx context.Context, reg imageledger.CleanupRegistry, entries []imageledger.Entry, tag, msgPrefix string) error {
	if err := imageledger.Cleanup(ctx, reg, entries, tag); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "%s: cleaned up candidate tags for %d entr(y/ies) (release tag %q)\n", msgPrefix, len(entries), tag)

	return nil
}
