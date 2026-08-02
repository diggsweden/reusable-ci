// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func ledgerValidateCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdValidate,
		Usage: "re-validate every entry in the ledger against the release tag (trust-boundary check)",
		Description: `EXAMPLE:
   reusable-ci container ledger validate --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			&cli.BoolFlag{Name: "non-empty", Usage: "fail when the ledger contains no entries"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.ValidateAll(entries, cmd.String(flagTag)); err != nil {
				return err
			}

			if cmd.Bool("non-empty") && len(entries) == 0 {
				return fmt.Errorf("imageledger: ledger must contain at least one entry: %w", errs.ErrValidation)
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) valid for release tag %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}

func ledgerVerifyDigestsCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify-digests",
		Usage: "re-verify each entry's recorded digest against what the registry serves (candidate_tag → final_tag → digest ref); distinct from `validate`, which checks entries against the release tag offline",
		Description: `EXAMPLE:
   reusable-ci container ledger verify-digests --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			data, err := cliio.ReadFile(cmd.String(flagLedger))
			if err != nil {
				return fmt.Errorf("ledger: read %s: %w", cmd.String(flagLedger), err)
			}

			entries, err := imageledger.Parse(data)
			if err != nil {
				return err
			}

			if err := imageledger.Verify(ctx, ociregistry.New(), entries, cmd.String(flagTag)); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) verified against the registry for %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}
