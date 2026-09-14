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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func ledgerRollbackCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRollback,
		Usage: "undo a release-tag promotion from its pre-promotion rollback journal",
		Description: `EXAMPLE:
	   reusable-ci container ledger rollback --journal image-promotions.jsonl --tag v1.2.3`,
		Flags: []cli.Flag{
			releaseTagFlag(),
			ledgerAuthFileFlag("registry auth file for the digest checks the rollback makes before deleting"),
			expectedImageRepositoryFlag(),
			promotionJournalFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			journal := cmd.String("journal")
			if journal == "" {
				return fmt.Errorf("ledger rollback: --journal is required because rollback without pre-promotion state cannot prove tag ownership: %w", errs.ErrUsage)
			}

			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				return ledgerRollbackFromJournal(ctx, cmd, dep, journal)
			})
		},
	}
}

// ledgerRollbackFromJournal undoes a promotion from its promotion journal:
// the journal records exactly which pointer tags the promotion created.
func ledgerRollbackFromJournal(ctx context.Context, cmd *cli.Command, dep *deps.Deps, journal string) error {
	records, err := loadPromotionJournal(journal, "ledger", cmd.String(flagExpectedImageRepository))
	if err != nil {
		return err
	}

	reg, err := promotionRollbackReg(dep, dryrun.Enabled(cmd), ledgerRegistry(cmd))
	if err != nil {
		return err
	}

	return runPromotionJournalRollback(ctx, reg, records, cmd.String(flagTag), "ledger")
}

// runPromotionJournalRollback is the shared rollback body for `ledger
// rollback --journal` and `release-images rollback`: undo the journaled
// promotion, then report with the caller's message prefix.
func runPromotionJournalRollback(ctx context.Context, reg imageledger.PromotionRollbackRegistry, records []imageledger.PromotionRecord, tag, msgPrefix string) error {
	if err := imageledger.RollbackReleasePromotion(ctx, reg, records, tag); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "%s: rolled back %d promotion journal entr(y/ies)\n", msgPrefix, len(records))

	return nil
}
