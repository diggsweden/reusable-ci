// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

func ledgerMergeCmd() *cli.Command {
	return &cli.Command{
		Name:      "merge",
		Usage:     "merge several per-container ledger files into one (drops exact duplicates); for promoting a multi-container release as a single unit",
		ArgsUsage: "<path>... (files, or directories searched for release-images.json)",
		Description: `EXAMPLE:
   reusable-ci container ledger merge dist/container-a dist/container-b --ledger release-images.json`,
		Flags: []cli.Flag{
			ledgerPathFlag(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			docs, err := readLedgerDocs(cmd.Args().Slice())
			if err != nil {
				return err
			}

			out, err := imageledger.Merge(docs)
			if err != nil {
				return err
			}

			// A merge that yields no entries means no ledger was found where one
			// was expected (e.g. the build's best-effort ledger upload didn't
			// run). Promotion will be a safe no-op, but surface it as a
			// forge-aware annotation (::warning:: on GitHub; a plain Warning:
			// line elsewhere, incl. Forgejo) so it is visible rather than a
			// silently green run.
			if entries, perr := imageledger.Parse(out); perr == nil && len(entries) == 0 {
				deps.Annotator(cmd).Warningf("ledger merge: no entries found under %v — promotion will be a no-op; check the build's ledger upload", cmd.Args().Slice())
			}

			path := cmd.String(flagLedger)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // dist dir; 0755 is conventional.
				return fmt.Errorf("ledger: create dir for %s: %w", path, err)
			}

			if err := cliio.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // ledger is public release metadata, not a secret.
				return err
			}

			_, _ = fmt.Fprintf(os.Stderr, "ledger: merged %d source(s) → %s\n", len(docs), path)

			return nil
		},
	}
}

// readLedgerDocs reads ledger JSON from each path: a file is read directly, a
// directory is walked for files named release-images.json (the per-container
// artifacts the promotion job downloads). Missing inputs are an error so a
// silent empty merge can't mask a broken hand-off.
func readLedgerDocs(paths []string) ([][]byte, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("ledger merge: at least one input path is required: %w", errs.ErrMissingInput)
	}

	var docs [][]byte

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// A missing input degrades to "no ledger here" rather than a hard
				// failure: when the build's best-effort ledger upload didn't run,
				// the promotion job finds no artifacts and must no-op (the image
				// still ships with its build tags) instead of reddening a release
				// whose signed artifacts already published.
				_, _ = fmt.Fprintf(os.Stderr, "ledger merge: %s not found, skipping\n", path)

				continue
			}

			return nil, fmt.Errorf("ledger merge: %s: %w", path, err)
		}

		if info.IsDir() {
			found, walkErr := readLedgerDir(path)
			if walkErr != nil {
				return nil, walkErr
			}

			docs = append(docs, found...)

			continue
		}

		data, readErr := cliio.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("ledger merge: read %s: %w", path, readErr)
		}

		docs = append(docs, data)
	}

	return docs, nil
}

// readLedgerDir walks dir and reads every file named defaultLedgerBasename —
// the per-container ledger artifacts the promotion job downloads into one
// directory tree.
func readLedgerDir(dir string) ([][]byte, error) {
	var docs [][]byte

	walkErr := filepath.WalkDir(dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || entry.Name() != defaultLedgerBasename {
			return nil
		}

		data, readErr := os.ReadFile(filePath) //nolint:gosec // operator-supplied ledger directory.
		if readErr != nil {
			return fmt.Errorf("read %s: %w", filePath, readErr)
		}

		docs = append(docs, data)

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("ledger merge: walk %s: %w", dir, walkErr)
	}

	return docs, nil
}
