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

func ledgerRollbackCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRollback,
		Usage: "undo a stage's promotion: delete that stage's pointer tag(s) (e.g. :dev/:staging/:release) that still serve the entry's digest; the immutable :<version> tag is never touched",
		Description: `EXAMPLE:
   reusable-ci container ledger rollback --ledger release-images.json --tag v1.2.3 --stage release`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			stageFlag(),
			stageRepoFlag(),
			releaseTagsFromLedgerFlag(),
			expectedImageRepositoryFlag(),
			promotionJournalFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(dep *deps.Deps) error {
				if journal := cmd.String("journal"); journal != "" {
					return ledgerRollbackFromJournal(ctx, cmd, dep, journal)
				}

				return ledgerRollbackFromLedger(ctx, cmd, dep)
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

	reg, err := promotionRollbackReg(dep, dryrun.Enabled(cmd))
	if err != nil {
		return err
	}

	return runPromotionJournalRollback(ctx, reg, records, cmd.String(flagTag), "ledger")
}

// ledgerRollbackFromLedger undoes a stage's promotion from the ledger
// itself when no promotion journal is available.
func ledgerRollbackFromLedger(ctx context.Context, cmd *cli.Command, dep *deps.Deps) error {
	entries, err := ledgerEntriesFromCmd(cmd)
	if err != nil {
		return err
	}

	reg, err := cleanupReg(dep, dryrun.Enabled(cmd))
	if err != nil {
		return err
	}

	stage := imageledger.Stage{
		Name:                cmd.String("stage"),
		TargetRepo:          cmd.String("stage-repo"),
		UseEntryReleaseTags: cmd.Bool("release-tags-from-ledger"),
	}
	if err := imageledger.RollbackStage(ctx, reg, entries, cmd.String(flagTag), stage); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "ledger: rolled back %d entr(y/ies) for stage %q\n", len(entries), stage.Name)

	return nil
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
