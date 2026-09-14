// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

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
			// An empty ledger is refused by default. "validate" is called as a
			// release gate (promote-stage.yml runs it immediately before
			// `promote`), and there an empty ledger is never legitimate: it
			// means the build job's `ledger add` did not run, or ran against a
			// different --ledger path. Passing that through reported success
			// for a release with no recorded images.
			&cli.BoolFlag{Name: "allow-empty", Usage: "accept a ledger with no entries (default: an empty ledger is a failure)"},
			// Retained: --non-empty now describes the default, so it is
			// accepted and does nothing rather than breaking callers that pass it.
			&cli.BoolFlag{Name: "non-empty", Usage: "deprecated, now the default; accepted and ignored", Hidden: true},
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

			if len(entries) == 0 && !cmd.Bool("allow-empty") {
				return fmt.Errorf(
					"imageledger: ledger %s contains no entries: nothing was recorded to validate "+
						"(pass --allow-empty if a release with no images is expected): %w",
					cmd.String(flagLedger), errs.ErrValidation)
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) valid for release tag %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}

func ledgerVerifyDigestsCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-digests",
		Usage: "re-validate each entry's recorded digest against what the registry serves (candidate_tag → final_tag → digest ref); distinct from `validate`, which checks entries against the release tag offline",
		Description: `EXAMPLE:
   reusable-ci container ledger validate-digests --ledger release-images.json --tag v1.2.3`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
			releaseTagFlag(),
			ledgerAuthFileFlag("registry auth file for the recorded-digest checks"),
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

			if err := imageledger.Verify(ctx, ledgerRegistry(cmd), entries, cmd.String(flagTag)); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: %d entr(y/ies) verified against the registry for %q\n", len(entries), cmd.String(flagTag))

			return nil
		},
	}
}
