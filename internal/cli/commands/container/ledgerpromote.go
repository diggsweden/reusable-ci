// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/dryrun"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func digestRefFallbackFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    "allow-digest-ref-fallback",
		Sources: cli.EnvVars("PROMOTE_ALLOW_DIGEST_REF_FALLBACK"),
		Usage:   "when candidate_tag is absent or no longer serves the recorded digest, copy from the ledger ref digest instead; recovers a re-run release whose candidate tag has since been cleaned up",
	}
}

func ledgerPromoteCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdPromote,
		Usage: "promote each entry's candidate image to the stage's moving pointer (<base>:<stage>) on the same digest, verifying after each copy; the immutable :<version> tag is build-only",
		Description: `EXAMPLES:
   # Promote to the :release pointer (release scope requires --tag)
   reusable-ci container ledger promote --ledger release-images.json --tag v1.2.3 --stage release

   # Preview a staging promotion without touching the registry
   reusable-ci container ledger promote --ledger release-images.json --stage staging --dry-run`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			ledgerAuthFileFlag("registry auth file for the promotion copies and digest verification"),
			stageFlag(),
			stageRepoFlag(),
			releaseTagsFromLedgerFlag(),
			digestRefFallbackFlag(),
			expectedImageRepositoryFlag(),
			promotionJournalFlag(),
			dryRunFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			entries, err := ledgerEntriesFromCmd(cmd)
			if err != nil {
				return err
			}

			stage := imageledger.Stage{
				Name:                   cmd.String("stage"),
				TargetRepo:             cmd.String("stage-repo"),
				UseEntryReleaseTags:    cmd.Bool("release-tags-from-ledger"),
				AllowDigestRefFallback: cmd.Bool("allow-digest-ref-fallback"),
			}

			// A cross-registry destination (--stage-repo) copies the
			// signature via cosign; same-repo promotions never consult it.
			var realSigCopier imageledger.SignatureCopier
			if stage.TargetRepo != "" {
				realSigCopier = cosignSigCopier{cosign: cosign.New(), out: os.Stderr}
			}

			reg, sigCopier := promotionRegistries(ledgerRegistry(cmd), dryrun.Enabled(cmd), realSigCopier)

			return runLedgerPromotion(ctx, promotionRun{
				reg:            reg,
				sigCopier:      sigCopier,
				entries:        entries,
				releaseTag:     cmd.String(flagTag),
				stage:          stage,
				journal:        cmd.String("journal"),
				journalDirPerm: 0o755, //nolint:mnd // dist-style state dir; 0755 is conventional.
				errPrefix:      "ledger",
				done:           fmt.Sprintf("ledger: promoted %d entr(y/ies) to stage %q", len(entries), stage.Name),
				out:            os.Stderr,
			})
		},
	}
}

// promotionRun carries the shared promote body's inputs; `ledger
// promote` and `release-images promote` both build one and call
// runLedgerPromotion.
type promotionRun struct {
	reg            imageledger.Registry
	sigCopier      imageledger.SignatureCopier
	entries        []imageledger.Entry
	releaseTag     string
	stage          imageledger.Stage
	journal        string      // empty: skip the journal write
	journalDirPerm os.FileMode // mode for a created journal parent dir
	errPrefix      string      // error/message prefix: "ledger" or "release images"
	done           string      // success line printed after promotion
	out            io.Writer   // success-line sink (stderr)
}

// promotionRegistries builds the promote registry pair: dry-run
// simulates copies in memory and previews them (the simulated registry
// doubles as the signature copier); a real run narrates each copy via
// auditCopier (audit trail) and uses the caller's signature copier
// (cosign for cross-registry promotion, nil when unused).
func promotionRegistries(resolver imageledger.Registry, dryRun bool, realSigCopier imageledger.SignatureCopier) (imageledger.Registry, imageledger.SignatureCopier) {
	if dryRun {
		sim := newDryRunRegistry(resolver, os.Stderr)

		return sim, sim
	}

	return auditCopier{Registry: resolver, out: os.Stderr}, realSigCopier
}

// runLedgerPromotion is the shared promote body: journal the promotion
// plan first (the rollback source of truth), promote every entry to the
// stage, then report the caller's success line.
func runLedgerPromotion(ctx context.Context, run promotionRun) error {
	if err := writePromotionJournal(ctx, run); err != nil {
		return err
	}

	if err := imageledger.PromoteToStage(ctx, run.reg, run.sigCopier, run.entries, run.releaseTag, run.stage); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(run.out, run.done)

	return nil
}

// writePromotionJournal plans the promotion and writes the journal file
// when a journal path is set; with no journal path it is a no-op.
func writePromotionJournal(ctx context.Context, run promotionRun) error {
	if run.journal == "" {
		return nil
	}

	records, err := imageledger.PlanReleasePromotionRollback(ctx, run.reg, run.entries, run.releaseTag, run.stage)
	if err != nil {
		return err
	}

	body, err := imageledger.MarshalPromotionJournal(records)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(run.journal), run.journalDirPerm); err != nil {
		return fmt.Errorf("%s: create dir for promotion journal %s: %w", run.errPrefix, run.journal, err)
	}

	if err := cliio.WriteFile(run.journal, body, 0o600); err != nil {
		return fmt.Errorf("%s: write promotion journal %s: %w", run.errPrefix, run.journal, err)
	}

	return nil
}
